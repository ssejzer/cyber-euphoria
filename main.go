package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"strings"
	"syscall/js"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	sampleRate = 44100
)

type Dictionary map[string]string

var translations = map[string]Dictionary{
	"en": {"win": "SYNERGY REACHED", "exit": "EXIT", "lvl": "LEVEL", "best": "SCORES", "lang": "LANG: EN", "combo": "COMBO", "streak": "STREAK", "over": "GAME OVER"},
	"es": {"win": "SINERGIA ALCANZADA", "exit": "SALIR", "lvl": "NIVEL", "best": "RECORDS", "lang": "IDIO: ES", "combo": "COMBO", "streak": "RACHA", "over": "JUEGO TERMINADO"},
	"ru": {"win": "SINERGIYA DOSTIGNUTA", "exit": "VYKHOD", "lvl": "UROVEN", "best": "REKORDY", "lang": "YAZYK: RU", "combo": "KOMBO", "streak": "SERIYA", "over": "KONETS IGRY"},
}
var langOrder = []string{"es", "en", "ru"}

type Zone struct {
	x, y, vx, vy float64
	power         float64
	radius        float64
}

type Particle struct {
	x, y, vx, vy float64
	life          float64
}

type ScoreEntry struct {
	name  string
	level int
}

type serverScore struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// HTTP helpers using synchronous XHR (same-origin, localhost)
func httpGetJSON(url string) string {
	xhr := js.Global().Get("XMLHttpRequest").New()
	xhr.Call("open", "GET", url, false)
	xhr.Call("send")
	if xhr.Get("status").Int() == 200 {
		return xhr.Get("responseText").String()
	}
	return ""
}

func httpPostJSON(url, body string) string {
	xhr := js.Global().Get("XMLHttpRequest").New()
	xhr.Call("open", "POST", url, false)
	xhr.Call("setRequestHeader", "Content-Type", "application/json")
	xhr.Call("send", body)
	if xhr.Get("status").Int() == 200 {
		return xhr.Get("responseText").String()
	}
	return ""
}

// --- Kage Shader (Neon Aesthetic) ---
var shaderSource = []byte(`
//kage:unit pixels
package main

var Time float
var Cursor vec2
var WinTime float
var ZoneType float

func Fragment(position vec4, texCoord vec2, color vec4) vec4 {
	p := (texCoord - 0.5) * 2.0
	dist := length(p)
	t := Time
	sync := Cursor.x
	winProgress := WinTime / 400.0

	radius := 0.2 + (0.12 * sin(t * 3.0) * sync) + (winProgress * 5.0)
	glow := 0.012 / abs(dist - radius)

	cyan := vec3(0.0, 0.8, 1.0)
	magenta := vec3(1.0, 0.1, 0.8)
	red := vec3(1.0, 0.0, 0.0)

	finalCol := mix(cyan, magenta, sync)
	if ZoneType > 0.5 {
		finalCol = mix(finalCol, vec3(1.0), 0.4)
	} else if ZoneType < -0.5 {
		finalCol = mix(finalCol, red, 0.6)
	}

	flash := step(0.98, sin(winProgress * 20.0)) * winProgress
	return vec4(mix(finalCol * glow, vec3(1.0), flash), glow + flash)
}
`)

type Game struct {
	lang             string
	level            int
	syncLevel        float64
	motionVal        float64
	win              bool
	winTime          float64
	time             float64
	shader           *ebiten.Shader
	lastX, lastY     int
	screenWidth      int
	screenHeight     int
	zones            []Zone
	particles        []*Particle
	audioCtx         *audio.Context
	audioStarted     bool
	music            *musicalEngine
	player           *audio.Player
	scores           []ScoreEntry
	isTouching       bool
	showLeaderboard  bool
	currentZonePower float64
	fingerX, fingerY float64
	fingerSpeed      float64
	comboCount       int
	milestones       [3]bool
	milestoneTime    float64
	milestoneMsg     string
	levelStreak      int
	timeLeft         float64
	gameOver         bool
	playerName       string
}

func levelDuration(level int) float64 {
	t := 25.0 - float64(level)
	if t < 15.0 {
		t = 15.0
	}
	return t
}

func (g *Game) Update() error {
	g.time += 1.0 / 60.0
	touchIDs := ebiten.AppendTouchIDs(nil)
	g.isTouching = len(touchIDs) > 0 || ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	if !g.audioStarted && g.isTouching {
		g.initAudio()
	}

	// Wait for Draw to set screen dimensions and initialize zones
	if g.screenWidth == 0 {
		return nil
	}

	justTouched := false
	var tx, ty int
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		justTouched = true
		tx, ty = ebiten.CursorPosition()
	} else {
		justIDs := inpututil.AppendJustPressedTouchIDs(nil)
		if len(justIDs) > 0 {
			justTouched = true
			tx, ty = ebiten.TouchPosition(justIDs[0])
		}
	}

	if justTouched {
		// EXIT always works
		if tx > g.screenWidth-100 && ty < 60 {
			js.Global().Get("window").Get("location").Call("reload")
		}
		// Tap anywhere (except EXIT) restarts after game over
		if g.gameOver {
			g.resetGame()
			return nil
		}
		if tx < 100 && ty < 60 {
			g.nextLang()
		}
		if tx > 110 && tx < 160 && ty < 60 {
			g.showLeaderboard = !g.showLeaderboard
		}
		if tx > 170 && tx < 230 && ty < 60 {
			js.Global().Get("localStorage").Call("removeItem", "cyber_player_name")
			g.playerName = ""
		}
	}

	if g.player != nil {
		vol := 0.0
		if g.isTouching || g.win {
			vol = g.syncLevel
			if g.win {
				vol = 1.0
			}
			if vol < 0.1 {
				vol = 0.1
			}
		}
		g.player.SetVolume(vol)
	}

	if g.gameOver {
		return nil
	}

	if g.win {
		g.winTime++
		if g.winTime == 1 {
			g.saveScore()
		}
		if g.winTime > 450 {
			g.nextLevel()
		}
		return nil
	}

	// Timer countdown — lose when it hits zero
	g.timeLeft -= 1.0 / 60.0
	if g.timeLeft <= 0 {
		g.timeLeft = 0
		g.gameOver = true
		g.saveScore()
		return nil
	}

	// Friction and Zone Logic
	friction := 0.0
	g.currentZonePower = 0.0
	g.fingerSpeed = 0
	if g.isTouching {
		var curX, curY int
		if len(touchIDs) > 0 {
			curX, curY = ebiten.TouchPosition(touchIDs[0])
		} else {
			curX, curY = ebiten.CursorPosition()
		}
		g.fingerX, g.fingerY = float64(curX), float64(curY)

		for i := range g.zones {
			z := &g.zones[i]
			dist := math.Sqrt(math.Pow(g.fingerX-z.x, 2) + math.Pow(g.fingerY-z.y, 2))
			if dist < z.radius {
				g.currentZonePower = z.power
				js.Global().Get("window").Get("navigator").Call("vibrate", 10)
				g.spawnParticles(g.fingerX, g.fingerY, 1)
				break
			}
		}

		if g.lastX != 0 || g.lastY != 0 {
			dx, dy := g.fingerX-float64(g.lastX), g.fingerY-float64(g.lastY)
			distMoved := math.Sqrt(dx*dx + dy*dy)
			g.fingerSpeed = distMoved
			if distMoved > 1.0 {
				mul := 1.0
				if g.currentZonePower != 0 {
					mul = math.Abs(g.currentZonePower)
				}
				if g.currentZonePower < 0 {
					friction = -distMoved * 0.0006 // stronger interference drain
					if g.comboCount > 0 {
						g.comboCount -= 4
					}
				} else {
					comboMul := 1.0 + float64(g.comboCount)/60.0 // up to 2x at max combo
					friction = distMoved * 0.00012 * mul * comboMul
					if g.comboCount < 60 {
						g.comboCount++
					}
				}
			} else {
				// Finger is still — must keep moving or sync drains
				friction = -(0.001 + g.syncLevel*0.004)
			}
		}
		g.lastX, g.lastY = int(g.fingerX), int(g.fingerY)
	} else {
		g.lastX, g.lastY = 0, 0
		if g.comboCount > 0 {
			g.comboCount -= 2
		}
		// Progressive drain: gentle when low, punishing when near goal
		drain := 0.025 + g.syncLevel*0.07
		g.syncLevel -= drain
	}

	g.syncLevel += friction + (g.motionVal * 0.005)
	if g.syncLevel < 0 {
		g.syncLevel = 0
	}
	if g.syncLevel > 1.0 {
		g.syncLevel = 1.0
		g.win = true
	}

	// Milestone celebrations at 25%, 50%, 75%
	if g.milestoneTime > 0 {
		g.milestoneTime--
	}
	thresholds := [3]float64{0.25, 0.50, 0.75}
	msgs := [3]string{"25% SYNC!", "50% SYNC!", "75% SYNC!"}
	for i, t := range thresholds {
		if !g.milestones[i] && g.syncLevel >= t {
			g.milestones[i] = true
			g.milestoneTime = 90
			g.milestoneMsg = msgs[i]
			js.Global().Get("window").Get("navigator").Call("vibrate", 50)
		}
	}

	// Move zones — positive zones start moving at level 3 (was 6)
	for i := range g.zones {
		z := &g.zones[i]
		if z.power > 0 && g.level < 3 {
			continue
		}
		z.x += z.vx
		z.y += z.vy
		if z.x < 50 || z.x > float64(g.screenWidth-50) {
			z.vx *= -1
		}
		if z.y < 100 || z.y > float64(g.screenHeight-100) {
			z.vy *= -1
		}
	}
	g.updateParticles()
	return nil
}

func (g *Game) nextLang() {
	for i, l := range langOrder {
		if l == g.lang {
			g.lang = langOrder[(i+1)%len(langOrder)]
			return
		}
	}
}

func (g *Game) initAudio() {
	if g.audioCtx == nil {
		g.audioCtx = audio.NewContext(sampleRate)
	}
	if g.audioCtx != nil && !g.audioStarted {
		g.music = &musicalEngine{game: g}
		var err error
		g.player, err = g.audioCtx.NewPlayer(g.music)
		if err == nil {
			g.player.SetVolume(0)
			g.player.Play()
			g.audioStarted = true
		}
	}
}

func (g *Game) nextLevel() {
	g.levelStreak++
	g.level++
	g.win = false
	g.winTime = 0
	g.syncLevel = 0
	g.comboCount = 0
	g.timeLeft = levelDuration(g.level)
	g.initZones()
}

func (g *Game) resetGame() {
	g.level = 0
	g.syncLevel = 0
	g.win = false
	g.winTime = 0
	g.gameOver = false
	g.levelStreak = 0
	g.comboCount = 0
	g.timeLeft = levelDuration(0)
	g.initZones()
}

func (g *Game) initZones() {
	rand.Seed(time.Now().UnixNano())
	g.zones = nil
	g.milestones = [3]bool{}
	speed := 1.0 + float64(g.level)*0.2
	radius := 90.0 - float64(g.level)*4.0
	if radius < 35.0 {
		radius = 35.0
	}
	for i := 0; i < 3; i++ {
		vx, vy := 0.0, 0.0
		if g.level >= 3 {
			vx = (rand.Float64() - 0.5) * speed
			vy = (rand.Float64() - 0.5) * speed
		}
		g.zones = append(g.zones, Zone{
			x: float64(rand.Intn(g.screenWidth-100) + 50),
			y: float64(rand.Intn(g.screenHeight-200) + 100),
			vx: vx, vy: vy,
			power:  1.6 + float64(g.level)*0.1,
			radius: radius,
		})
	}
	// Start with 2 interference zones, cap at 6
	numNeg := 2 + g.level/2
	if numNeg > 6 {
		numNeg = 6
	}
	for i := 0; i < numNeg; i++ {
		g.zones = append(g.zones, Zone{
			x:      float64(rand.Intn(g.screenWidth-100) + 50),
			y:      float64(rand.Intn(g.screenHeight-200) + 100),
			vx:     (rand.Float64() - 0.5) * (speed + 1.5),
			vy:     (rand.Float64() - 0.5) * (speed + 1.5),
			power:  -5.0,
			radius: 80,
		})
	}
	g.loadScores()
}

func (g *Game) getPlayerName() string {
	stored := js.Global().Get("localStorage").Call("getItem", "cyber_player_name").String()
	if stored != "null" && stored != "" {
		return stored
	}
	result := js.Global().Call("prompt", "ENTER YOUR NAME:", "PLAYER")
	name := "PLAYER"
	if !result.IsNull() && !result.IsUndefined() {
		n := strings.TrimSpace(result.String())
		if n != "" {
			name = n
		}
	}
	if len(name) > 8 {
		name = name[:8]
	}
	name = strings.ToUpper(name)
	js.Global().Get("localStorage").Call("setItem", "cyber_player_name", name)
	return name
}

func (g *Game) loadScores() {
	resp := httpGetJSON("/leaderboard")
	if resp == "" {
		return
	}
	var entries []serverScore
	if err := json.Unmarshal([]byte(resp), &entries); err != nil {
		return
	}
	g.scores = nil
	for i, e := range entries {
		if i >= 5 {
			break
		}
		g.scores = append(g.scores, ScoreEntry{name: e.Name, level: e.Level})
	}
}

func (g *Game) saveScore() {
	if g.playerName == "" {
		g.playerName = g.getPlayerName()
	}
	payload := serverScore{Name: g.playerName, Level: g.level + 1}
	data, _ := json.Marshal(payload)
	resp := httpPostJSON("/leaderboard", string(data))
	if resp == "" {
		return
	}
	var entries []serverScore
	if err := json.Unmarshal([]byte(resp), &entries); err != nil {
		return
	}
	g.scores = nil
	for i, e := range entries {
		if i >= 5 {
			break
		}
		g.scores = append(g.scores, ScoreEntry{name: e.Name, level: e.Level})
	}
}

func (g *Game) spawnParticles(x, y float64, n int) {
	for i := 0; i < n; i++ {
		g.particles = append(g.particles, &Particle{
			x: x, y: y,
			vx:   (rand.Float64() - 0.5) * 5,
			vy:   (rand.Float64() - 0.5) * 5,
			life: 1.0,
		})
	}
}

func (g *Game) updateParticles() {
	for i := len(g.particles) - 1; i >= 0; i-- {
		p := g.particles[i]
		p.x += p.vx
		p.y += p.vy
		p.life -= 0.04
		if p.life <= 0 {
			g.particles = append(g.particles[:i], g.particles[i+1:]...)
		}
	}
}

// --- Musical Synthesizer Engine ---
// Pentatonic C major scale (C4–E5)
var pentatonic = []float64{
	261.63, // C4
	293.66, // D4
	329.63, // E4
	392.00, // G4
	440.00, // A4
	523.25, // C5
	587.33, // D5
	659.25, // E5
}

// 16-step melodic pattern (indices into pentatonic[])
var melody = []int{0, 2, 4, 5, 4, 2, 4, 7, 0, 2, 4, 5, 7, 5, 4, 2}

type musicalEngine struct {
	game     *Game
	phase    float64 // sine oscillator phase in radians
	noteFrac float64 // 0..1 progress within current note
	noteIdx  int
}

func (m *musicalEngine) Read(p []byte) (int, error) {
	for i := 0; i < len(p)/4; i++ {
		// Tempo: 80 BPM at sync=0 → 200 BPM at sync=1, eighth notes
		bpm := 80.0 + m.game.syncLevel*120.0
		noteAdvance := bpm / (float64(sampleRate) * 60.0 * 2.0)

		m.noteFrac += noteAdvance
		if m.noteFrac >= 1.0 {
			m.noteFrac -= 1.0
			m.noteIdx = (m.noteIdx + 1) % len(melody)
			m.phase = 0 // clean phase reset per note avoids clicks
		}

		// Envelope: fast attack → slight decay sustain → release
		frac := m.noteFrac
		var env float64
		switch {
		case frac < 0.05:
			env = frac / 0.05
		case frac < 0.75:
			env = 1.0 - (frac-0.05)/0.70*0.2 // 1.0 → 0.8
		default:
			env = 0.8 * (1.0 - (frac-0.75)/0.25)
		}

		// Sine + harmonics for a bell-like timbre
		freq := pentatonic[melody[m.noteIdx]]
		m.phase += 2 * math.Pi * freq / float64(sampleRate)
		if m.phase > 2*math.Pi {
			m.phase -= 2 * math.Pi
		}
		sample := (math.Sin(m.phase)*0.45 + math.Sin(m.phase*2)*0.12 + math.Sin(m.phase*3)*0.04) * env

		// Binaural pan based on finger X position
		pan := 0.5
		if m.game.screenWidth > 0 {
			pan = m.game.fingerX / float64(m.game.screenWidth)
			if pan < 0 {
				pan = 0
			}
			if pan > 1 {
				pan = 1
			}
		}
		sl := int16(sample * (1.0 - pan*0.5) * 32767)
		sr := int16(sample * (0.5 + pan*0.5) * 32767)
		p[4*i] = byte(sl)
		p[4*i+1] = byte(sl >> 8)
		p[4*i+2] = byte(sr)
		p[4*i+3] = byte(sr >> 8)
	}
	return len(p), nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	if g.screenWidth == 0 {
		g.screenWidth, g.screenHeight = w, h
		g.timeLeft = levelDuration(0)
		g.initZones()
	}

	if g.shader != nil {
		op := &ebiten.DrawRectShaderOptions{}
		zoneType := 0.0
		ids := ebiten.AppendTouchIDs(nil)
		if len(ids) > 0 {
			tx, ty := ebiten.TouchPosition(ids[0])
			for _, z := range g.zones {
				if math.Sqrt(math.Pow(float64(tx)-z.x, 2)+math.Pow(float64(ty)-z.y, 2)) < z.radius {
					zoneType = z.power
					break
				}
			}
		}
		op.Uniforms = map[string]interface{}{
			"Time":     float32(g.time),
			"Cursor":   []float32{float32(g.syncLevel), 0},
			"WinTime":  float32(g.winTime),
			"ZoneType": float32(zoneType),
		}
		screen.DrawRectShader(w, h, g.shader, op)
	}

	for _, p := range g.particles {
		vector.DrawFilledCircle(screen, float32(p.x), float32(p.y), 2*float32(p.life), color.RGBA{255, 255, 255, 180}, true)
	}

	dict := translations[g.lang]
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf(">> %s: %d", dict["lvl"], g.level+1), 20, 75)
	if g.comboCount > 5 && !g.win && !g.showLeaderboard && !g.gameOver {
		comboMul := 1.0 + float64(g.comboCount)/60.0
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%s %.1fx", dict["combo"], comboMul), 20, 95)
	}

	if g.gameOver || g.win || g.showLeaderboard {
		boxH := float32(240)
		if g.gameOver {
			boxH = 280
		}
		vector.DrawFilledRect(screen, 40, float32(h/2-100), float32(w-80), boxH, color.RGBA{0, 0, 0, 220}, true)

		title := dict["best"]
		if g.win {
			title = dict["win"]
		}
		if g.gameOver {
			title = dict["over"]
		}
		ebitenutil.DebugPrintAt(screen, title, w/2-60, h/2-80)

		for i, s := range g.scores {
			ebitenutil.DebugPrintAt(screen,
				fmt.Sprintf("%d. %-8s %s %d", i+1, s.name, dict["lvl"], s.level),
				w/2-70, h/2-40+i*20)
		}

		if (g.win || g.gameOver) && g.levelStreak > 0 {
			ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%s: %d", dict["streak"], g.levelStreak), w/2-40, h/2+65)
		}
		if g.gameOver {
			ebitenutil.DebugPrintAt(screen, ">> TAP TO RETRY <<", w/2-72, h/2+90)
		}
	} else {
		barW := float32(w - 60)
		barY := float32(h - 40)
		vector.DrawFilledRect(screen, 30, barY, barW, 6, color.RGBA{40, 40, 40, 150}, true)
		vector.DrawFilledRect(screen, 30, barY, barW*float32(g.syncLevel), 6, color.RGBA{255, 255, 255, 220}, true)
		// Milestone tick marks at 25%, 50%, 75%
		for _, t := range []float32{0.25, 0.50, 0.75} {
			mx := float32(30) + barW*t
			vector.DrawFilledRect(screen, mx-1, barY-2, 2, 10, color.RGBA{0, 200, 255, 180}, true)
		}

		// Timer bar — green → orange → red as time runs out
		timerFrac := float32(g.timeLeft) / float32(levelDuration(g.level))
		timerCol := color.RGBA{0, 200, 100, 200}
		if g.timeLeft < 5.0 {
			timerCol = color.RGBA{255, 50, 50, 220}
		} else if g.timeLeft < 10.0 {
			timerCol = color.RGBA{255, 180, 0, 200}
		}
		vector.DrawFilledRect(screen, 0, 62, float32(w)*timerFrac, 4, timerCol, true)
	}

	// Milestone popup overlay
	if g.milestoneTime > 0 && !g.win && !g.gameOver {
		alpha := float64(g.milestoneTime) / 90.0
		vector.DrawFilledRect(screen, 0, 0, float32(w), float32(h), color.RGBA{0, 200, 255, uint8(alpha * 35)}, true)
		ebitenutil.DebugPrintAt(screen, g.milestoneMsg, w/2-35, h/2-10)
	}

	// HEADER BUTTONS
	// EXIT (Red)
	vector.DrawFilledRect(screen, float32(w-80), 10, 70, 40, color.RGBA{180, 0, 50, 200}, true)
	ebitenutil.DebugPrintAt(screen, dict["exit"], w-70, 25)
	// LANGUAGE (Blue)
	vector.DrawFilledRect(screen, 10, 10, 90, 40, color.RGBA{0, 100, 200, 200}, true)
	ebitenutil.DebugPrintAt(screen, dict["lang"], 20, 25)
	// NAME reset (Teal)
	vector.DrawFilledRect(screen, 170, 10, 60, 40, color.RGBA{0, 140, 120, 200}, true)
	nameLabel := g.playerName
	if nameLabel == "" {
		nameLabel = "NAME?"
	}
	ebitenutil.DebugPrintAt(screen, nameLabel, 178, 25)
	// SCORES (Golden + Trophy Icon)
	vector.DrawFilledRect(screen, 110, 10, 50, 40, color.RGBA{200, 150, 0, 200}, true)
	tx, ty := float32(135), float32(30)
	vector.DrawFilledRect(screen, tx-6, ty+5, 12, 2, color.White, true)
	vector.StrokeLine(screen, tx, ty+5, tx, ty, 2, color.White, true)
	vector.DrawFilledCircle(screen, tx, ty-4, 5, color.White, true)
}

func (g *Game) Layout(w, h int) (int, int) { return w, h }

func main() {
	s, err := ebiten.NewShader(shaderSource)
	if err != nil {
		panic(err)
	}
	game := &Game{lang: "es", shader: s}
	ebiten.SetWindowSize(400, 800)
	if err := ebiten.RunGame(game); err != nil {
		panic(err)
	}
}
