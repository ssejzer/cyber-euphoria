package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	_ "image/jpeg"
	"math"
	"math/rand"
	"strings"
	"syscall/js"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	sampleRate  = 44100
	uiBarHeight = 60
)

type Dictionary map[string]string

var translations = map[string]Dictionary{
	"en": {"win": "SYNERGY REACHED", "exit": "EXIT", "lvl": "LEVEL", "best": "SCORES", "lang": "LANG: EN", "combo": "COMBO", "streak": "STREAK", "over": "GAME OVER"},
	"es": {"win": "SINERGIA ALCANZADA", "exit": "SALIR", "lvl": "NIVEL", "best": "RECORDS", "lang": "IDIO: ES", "combo": "COMBO", "streak": "RACHA", "over": "JUEGO TERMINADO"},
	"fr": {"win": "SYNERGIE ATTEINTE", "exit": "QUITTER", "lvl": "NIVEAU", "best": "SCORES", "lang": "LANG: FR", "combo": "COMBO", "streak": "SERIE", "over": "PARTIE TERMINEE"},
	"ru": {"win": "SINERGIYA DOSTIGNUTA", "exit": "VYKHOD", "lvl": "UROVEN", "best": "REKORDY", "lang": "YAZYK: RU", "combo": "KOMBO", "streak": "SERIYA", "over": "KONETS IGRY"},
}
var langOrder = []string{"en", "es", "fr", "ru"}

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
	music            *voiceEngine
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
	milestoneColor   color.RGBA
	levelStreak      int
	timeLeft         float64
	gameOver         bool
	playerName       string
	gameStarted      bool
	bgImage          *ebiten.Image
	introImage       *ebiten.Image
	introAudio      js.Value
	introAudioReady bool
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
	// Pick up intro audio as soon as the HTML click handler has created it
	if !g.introAudioReady {
		intro := js.Global().Get("cyberIntroAudio")
		if !intro.IsUndefined() && !intro.IsNull() {
			g.introAudio = intro
			g.introAudioReady = true
		}
	}

	if g.screenWidth == 0 {
		return nil
	}

	// Update finger position early so header/play-area detection is current this frame
	if g.isTouching {
		if len(touchIDs) > 0 {
			cx, cy := ebiten.TouchPosition(touchIDs[0])
			g.fingerX, g.fingerY = float64(cx), float64(cy)
		} else {
			cx, cy := ebiten.CursorPosition()
			g.fingerX, g.fingerY = float64(cx), float64(cy)
		}
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
		if tx > g.screenWidth-100 && ty < uiBarHeight {
			js.Global().Get("window").Get("location").Call("reload")
		}
		// Retry after game over
		if g.gameOver {
			g.resetGame()
			return nil
		}
		// Language cycle (works on intro and in-game)
		if tx < 100 && ty < uiBarHeight {
			g.nextLang()
		}
		if g.gameStarted {
			// Scores toggle
			if tx > 110 && tx < 160 && ty < uiBarHeight {
				g.showLeaderboard = !g.showLeaderboard
			}
			// NAME button: immediately prompt for new name
			if tx > 170 && tx < 230 && ty < uiBarHeight {
				js.Global().Get("localStorage").Call("removeItem", "cyber_player_name")
				g.playerName = g.getPlayerName()
			}
		}
		// Tap play area to start (intro screen)
		if !g.gameStarted && ty >= uiBarHeight {
			if g.playerName == "" {
				g.playerName = g.getPlayerName()
				// Don't start yet — show name on intro, next tap starts
			} else {
				g.gameStarted = true
			}
		}
	}

	// Audio only when touching play area (not header) or during win
	touchingPlay := g.isTouching && g.fingerY >= uiBarHeight
	if g.player != nil {
		vol := 0.0
		if g.gameStarted && (touchingPlay || g.win) {
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
	if g.introAudioReady {
		v := g.introAudio.Get("volume").Float()
		if !g.gameStarted {
			if v < 0.7 {
				g.introAudio.Set("volume", math.Min(v+0.004, 0.7))
			}
		} else {
			if v > 0.12 {
				g.introAudio.Set("volume", math.Max(v-0.006, 0.12))
			}
		}
	}

	if g.gameOver {
		return nil
	}

	if g.win {
		g.winTime++
		if g.winTime == 1 {
			g.saveScore()
		}
		if g.winTime > 112 {
			g.nextLevel()
		}
		return nil
	}

	// Freeze game logic until player starts
	if !g.gameStarted {
		return nil
	}

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
		if g.fingerY < uiBarHeight {
			// Header touch — no gameplay, reset movement tracking
			g.lastX, g.lastY = 0, 0
		} else {
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
						friction = -distMoved * 0.0006
						if g.comboCount > 0 {
							g.comboCount -= 4
						}
					} else {
						comboMul := 1.0 + float64(g.comboCount)/60.0
						friction = distMoved * 0.00012 * mul * comboMul
						if g.comboCount < 60 {
							g.comboCount++
						}
					}
				} else {
					// Still — must keep moving or sync drains
					friction = -(0.001 + g.syncLevel*0.004)
				}
			}
			g.lastX, g.lastY = int(g.fingerX), int(g.fingerY)
		}
	} else {
		g.lastX, g.lastY = 0, 0
		if g.comboCount > 0 {
			g.comboCount -= 2
		}
		drain := 0.025 + g.syncLevel*0.07
		g.syncLevel -= drain
	}

	g.syncLevel += friction
	if g.syncLevel < 0 {
		g.syncLevel = 0
	}
	if g.syncLevel > 1.0 {
		g.syncLevel = 1.0
		g.win = true
	}

	if g.milestoneTime > 0 {
		g.milestoneTime--
	}
	thresholds := [3]float64{0.25, 0.50, 0.75}
	msgs := [3]string{"25% SYNC!", "50% SYNC!", "75% SYNC!"}
	colors := [3]color.RGBA{
		{0, 220, 255, 255},   // 25%: cyan
		{180, 0, 255, 255},   // 50%: purple
		{255, 0, 180, 255},   // 75%: magenta
	}
	for i, t := range thresholds {
		if !g.milestones[i] && g.syncLevel >= t {
			g.milestones[i] = true
			g.milestoneTime = 90
			g.milestoneMsg = msgs[i]
			g.milestoneColor = colors[i]
			js.Global().Get("window").Get("navigator").Call("vibrate", 50)
		}
	}

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
		g.music = &voiceEngine{game: g}
		var err error
		g.player, err = g.audioCtx.NewPlayer(g.music)
		if err == nil {
			g.player.SetVolume(0)
			g.player.Play()
		}
		g.audioStarted = true

		// Intro music via native HTML Audio — reliable relative URL resolution and MP3 support
		// Pick up the Audio element created by the HTML click handler (user-gesture context)
		intro := js.Global().Get("cyberIntroAudio")
		if !intro.IsUndefined() && !intro.IsNull() {
			g.introAudio = intro
			g.introAudioReady = true
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
	// Pre-load stored name so intro screen can show it immediately
	if g.playerName == "" {
		val := js.Global().Get("localStorage").Call("getItem", "cyber_player_name")
		if !val.IsNull() && !val.IsUndefined() {
			if stored := val.String(); stored != "" {
				g.playerName = stored
			}
		}
	}
	g.loadScores()
}

func (g *Game) getPlayerName() string {
	val := js.Global().Get("localStorage").Call("getItem", "cyber_player_name")
	if !val.IsNull() && !val.IsUndefined() {
		if stored := val.String(); stored != "" {
			return stored
		}
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
	resp := httpGetJSON(js.Global().Get("gameBaseURL").String()+"leaderboard.php")
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
	resp := httpPostJSON(js.Global().Get("gameBaseURL").String()+"leaderboard.php", string(data))
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

// --- Voice Engine ---
// Gameplay: low moan that rises in pitch and pace with syncLevel.
// Win: rapid climax arc — pitch peaks then settles.

type voiceEngine struct {
	game        *Game
	phase       float64
	vibratoPhase float64
	breathPhase  float64
	breathAcc    float64
	lpState     float64 // one-pole low-pass filter state
	wasWin      bool // detects win transition to reset state
	wasPlaying  bool // detects touch-start to sync breath phase
}

func (m *voiceEngine) Read(p []byte) (int, error) {
	for i := 0; i < len(p)/4; i++ {
		sync := m.game.syncLevel
		isWin := m.game.win
		winTime := m.game.winTime
		isPlaying := m.game.isTouching || isWin

		// Reset breath to peak on first touch — moan starts immediately audible
		if isPlaying && !m.wasPlaying {
			m.breathPhase = math.Pi / 2
		}
		m.wasPlaying = isPlaying

		// Reset and re-sync on win start — clear break from gameplay moan
		if isWin && !m.wasWin {
			m.breathPhase = math.Pi / 2
			m.phase = 0
		}
		m.wasWin = isWin

		var baseFreq, breathRate, vibratoAmp float64

		if isWin {
			winP := math.Min(1.0, winTime/112.0)
			// Rapid staccato panting: starts fast, peaks, then settles
			breathRate = 3.0 + winP*0.5 // 3.0 → 3.5 Hz (fast from the first moment)
			// Pitch arc: starts high, peaks, then resolves
			var curve float64
			if winP < 0.35 {
				curve = winP / 0.35
			} else if winP < 0.65 {
				curve = 1.0
			} else {
				curve = 1.0 - (winP-0.65)/0.35*0.45
			}
			baseFreq = 320.0 + curve*200.0  // 320 → 520 Hz at peak (clearly higher than gameplay)
			vibratoAmp = 0.02 + curve*0.03   // trembling at climax
		} else {
			// Gameplay: female voice range, rises with sync
			baseFreq = 180.0 + sync*180.0   // 180 → 360 Hz (female range)
			breathRate = 0.7 + sync*1.1     // 0.7 → 1.8 Hz
			vibratoAmp = 0.012 + sync*0.018 // expressive throughout
		}

		// Vibrato LFO at 5.5 Hz
		m.vibratoPhase += 2 * math.Pi * 5.5 / float64(sampleRate)
		if m.vibratoPhase > 2*math.Pi {
			m.vibratoPhase -= 2 * math.Pi
		}
		vibrato := 1.0 + vibratoAmp*math.Sin(m.vibratoPhase)

		// Breath rhythm: half-sine pulses
		// Win: staccato (0→1, clear gaps)
		// Gameplay: continuous (0.15→1)
		m.breathPhase += 2 * math.Pi * breathRate / float64(sampleRate)
		if m.breathPhase > 2*math.Pi {
			m.breathPhase -= 2 * math.Pi
		}
		breathRaw := math.Max(0, math.Sin(m.breathPhase))
		var breathEnv float64
		if isWin {
			breathEnv = breathRaw
		} else {
			breathEnv = 0.15 + 0.85*breathRaw
		}

		// Friction: movement speed intensifies the moan (more motion = louder/fuller)
		if !isWin {
			speedNorm := math.Min(1.0, m.game.fingerSpeed/25.0)
			breathEnv *= 0.35 + 0.65*speedNorm
		}

		// Pitch rises with breath amplitude (natural vocal inflection)
		pitchInflect := 1.0 + 0.04*breathRaw
		freq := baseFreq * vibrato * pitchInflect

		m.phase += 2 * math.Pi * freq / float64(sampleRate)
		if m.phase > 2*math.Pi {
			m.phase -= 2 * math.Pi
		}

		// Harmonic series — fewer upper harmonics to reduce buzz
		sample := (math.Sin(m.phase)*0.55 +
			math.Sin(m.phase*2)*0.28 + // strong 2nd — characteristic of female voice
			math.Sin(m.phase*3)*0.11 +
			math.Sin(m.phase*4)*0.05 +
			math.Sin(m.phase*5)*0.01) * breathEnv

		// Airy breath noise — reduced to avoid buzziness
		m.breathAcc = (m.breathAcc + (rand.Float64()*2-1)*0.04) * 0.97
		sample += m.breathAcc * 0.02 * breathEnv

		// Soft clip — reduced drive to avoid adding distortion harmonics
		sample = math.Tanh(sample * 0.75)

		// One-pole low-pass filter to smooth out high-frequency buzz
		// Cutoff rises with sync: 800 Hz at rest → 1600 Hz at full sync
		fc := 800.0 + sync*800.0
		alpha := 1.0 - math.Exp(-2*math.Pi*fc/float64(sampleRate))
		m.lpState += alpha * (sample - m.lpState)
		sample = m.lpState

		// Binaural pan
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

	if g.bgImage != nil {
		bw := float64(g.bgImage.Bounds().Dx())
		bh := float64(g.bgImage.Bounds().Dy())
		sx, sy := float64(w)/bw, float64(h)/bh
		s := sx
		if sy > sx {
			s = sy
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(s, s)
		op.GeoM.Translate((float64(w)-bw*s)/2, (float64(h)-bh*s)/2)
		screen.DrawImage(g.bgImage, op)
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

	if g.gameStarted {
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf(">> %s: %d", dict["lvl"], g.level+1), 20, 75)
		if g.comboCount > 5 && !g.win && !g.showLeaderboard && !g.gameOver {
			comboMul := 1.0 + float64(g.comboCount)/60.0
			ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%s %.1fx", dict["combo"], comboMul), 20, 95)
		}
	}

	if g.gameOver || g.win || (g.showLeaderboard && g.gameStarted) {
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
	} else if g.gameStarted {
		barW := float32(w - 60)
		barY := float32(h - 40)
		vector.DrawFilledRect(screen, 30, barY, barW, 6, color.RGBA{40, 40, 40, 150}, true)
		vector.DrawFilledRect(screen, 30, barY, barW*float32(g.syncLevel), 6, color.RGBA{255, 255, 255, 220}, true)
		for _, t := range []float32{0.25, 0.50, 0.75} {
			mx := float32(30) + barW*t
			vector.DrawFilledRect(screen, mx-1, barY-2, 2, 10, color.RGBA{0, 200, 255, 180}, true)
		}
		timerFrac := float32(g.timeLeft) / float32(levelDuration(g.level))
		timerCol := color.RGBA{0, 200, 100, 200}
		if g.timeLeft < 5.0 {
			timerCol = color.RGBA{255, 50, 50, 220}
		} else if g.timeLeft < 10.0 {
			timerCol = color.RGBA{255, 180, 0, 200}
		}
		vector.DrawFilledRect(screen, 0, 62, float32(w)*timerFrac, 4, timerCol, true)
	}

	if g.milestoneTime > 0 && !g.win && !g.gameOver {
		t := g.milestoneTime / 90.0  // 1→0
		progress := 1.0 - t          // 0→1
		cx := float32(w) / 2
		cy := float32(h) / 2
		maxR := float32(math.Sqrt(float64(w*w+h*h)) / 2)
		mc := g.milestoneColor

		// Primary ring: ease-out expansion, fades as it expands
		r1 := maxR * float32(1.0-math.Pow(1.0-progress, 2))
		mc.A = uint8(t * 255)
		vector.StrokeCircle(screen, cx, cy, r1, 4, mc, true)

		// Secondary ring: delayed by 15% of animation
		if progress > 0.15 {
			p2 := (progress - 0.15) / 0.85
			r2 := maxR * float32(1.0-math.Pow(1.0-p2, 2))
			mc.A = uint8(t * 160)
			vector.StrokeCircle(screen, cx, cy, r2, 2, mc, true)
		}

		// Screen-edge flash: only at the start of the animation
		if t > 0.75 {
			mc.A = uint8((t - 0.75) / 0.25 * 100)
			thick := float32(8)
			vector.DrawFilledRect(screen, 0, 0, float32(w), thick, mc, false)
			vector.DrawFilledRect(screen, 0, float32(h)-thick, float32(w), thick, mc, false)
			vector.DrawFilledRect(screen, 0, thick, thick, float32(h)-2*thick, mc, false)
			vector.DrawFilledRect(screen, float32(w)-thick, thick, thick, float32(h)-2*thick, mc, false)
		}

		// Text: visible in the middle of the animation
		if t > 0.15 && t < 0.8 {
			vector.DrawFilledRect(screen, float32(w/2-52), float32(h/2-18), 104, 22, color.RGBA{0, 0, 0, 160}, false)
			ebitenutil.DebugPrintAt(screen, g.milestoneMsg, w/2-35, h/2-10)
		}
	}

	// Intro screen overlay
	if !g.gameStarted {
		if g.introImage != nil {
			iw := float64(g.introImage.Bounds().Dx())
			ih := float64(g.introImage.Bounds().Dy())
			maxW := float64(w) * 0.85
			maxH := float64(h) * 0.60
			s := maxW / iw
			if ih*s > maxH {
				s = maxH / ih
			}
			imgW := iw * s
			imgH := ih * s
			ox := (float64(w) - imgW) / 2
			oy := float64(h) * 0.05
			iop := &ebiten.DrawImageOptions{}
			iop.GeoM.Scale(s, s)
			iop.GeoM.Translate(ox, oy)
			screen.DrawImage(g.introImage, iop)

			textY := int(oy+imgH) + 15
			vector.DrawFilledRect(screen, 20, float32(textY-8), float32(w-40), 90, color.RGBA{0, 0, 0, 180}, true)
			if g.playerName != "" {
				ebitenutil.DebugPrintAt(screen, "PLAYER: "+g.playerName, w/2-52, textY)
				ebitenutil.DebugPrintAt(screen, ">> TAP TO START <<", w/2-72, textY+20)
			} else {
				ebitenutil.DebugPrintAt(screen, ">> TAP TO ENTER NAME <<", w/2-88, textY)
			}
			ebitenutil.DebugPrintAt(screen, "MOVE FINGER IN GLOWING", w/2-88, textY+45)
			ebitenutil.DebugPrintAt(screen, " ZONES TO FILL THE BAR", w/2-88, textY+65)
		} else {
			vector.DrawFilledRect(screen, 30, float32(h/2-130), float32(w-60), 260, color.RGBA{0, 0, 0, 210}, true)
			ebitenutil.DebugPrintAt(screen, "CYBER EUPHORIA", w/2-56, h/2-110)
			if g.playerName != "" {
				ebitenutil.DebugPrintAt(screen, "PLAYER: "+g.playerName, w/2-52, h/2-80)
				ebitenutil.DebugPrintAt(screen, ">> TAP TO START <<", w/2-72, h/2-40)
			} else {
				ebitenutil.DebugPrintAt(screen, ">> TAP TO ENTER NAME <<", w/2-88, h/2-80)
			}
			ebitenutil.DebugPrintAt(screen, "MOVE FINGER IN GLOWING", w/2-88, h/2-10)
			ebitenutil.DebugPrintAt(screen, " ZONES TO FILL THE BAR", w/2-88, h/2+10)
		}
	}

	// HEADER BUTTONS
	vector.DrawFilledRect(screen, float32(w-80), 10, 70, 40, color.RGBA{180, 0, 50, 200}, true)
	ebitenutil.DebugPrintAt(screen, dict["exit"], w-70, 25)
	vector.DrawFilledRect(screen, 10, 10, 90, 40, color.RGBA{0, 100, 200, 200}, true)
	ebitenutil.DebugPrintAt(screen, dict["lang"], 20, 25)
	vector.DrawFilledRect(screen, 110, 10, 50, 40, color.RGBA{200, 150, 0, 200}, true)
	tx, ty := float32(135), float32(30)
	vector.DrawFilledRect(screen, tx-6, ty+5, 12, 2, color.White, true)
	vector.StrokeLine(screen, tx, ty+5, tx, ty, 2, color.White, true)
	vector.DrawFilledCircle(screen, tx, ty-4, 5, color.White, true)
	vector.DrawFilledRect(screen, 170, 10, 60, 40, color.RGBA{0, 140, 120, 200}, true)
	nameLabel := g.playerName
	if nameLabel == "" {
		nameLabel = "NAME?"
	}
	ebitenutil.DebugPrintAt(screen, nameLabel, 178, 25)
}

func (g *Game) Layout(w, h int) (int, int) { return w, h }

func main() {
	s, err := ebiten.NewShader(shaderSource)
	if err != nil {
		panic(err)
	}
	bg, _ := ebitenutil.NewImageFromURL("cyber-background.jpg")
	intro, _ := ebitenutil.NewImageFromURL("cyber-euphoria.jpg")
	game := &Game{lang: "en", shader: s, bgImage: bg, introImage: intro}
	ebiten.SetWindowSize(400, 800)
	if err := ebiten.RunGame(game); err != nil {
		panic(err)
	}
}
