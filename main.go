package main

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"strconv"
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

// --- Translations (Transliterated for font compatibility) ---
type Dictionary map[string]string
var translations = map[string]Dictionary{
	"en": {"title": "CYBER EUPHORIA", "win": "SYNERGY REACHED", "exit": "EXIT", "lvl": "LEVEL", "best": "SCORES", "lang": "LANG: EN"},
	"es": {"title": "CIBER EUFORIA", "win": "SINERGIA ALCANZADA", "exit": "SALIR", "lvl": "NIVEL", "best": "RECORDS", "lang": "IDIO: ES"},
	"ru": {"title": "KIBER EYFORIYA", "win": "SINERGIYA DOSTIGNUTA", "exit": "VYKHOD", "lvl": "UROVEN", "best": "REKORDY", "lang": "YAZYK: RU"},
}
var langOrder = []string{"es", "en", "ru"}

type Zone struct {
	x, y, vx, vy float64
	power        float64
	radius       float64
}

type Particle struct {
	x, y, vx, vy float64
	life         float64
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
	
	// Dynamic core radius
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
	lang           string
	level          int
	syncLevel      float64
	motionVal      float64
	win            bool
	winTime        float64
	time           float64
	shader         *ebiten.Shader
	lastX, lastY   int
	screenWidth    int
	screenHeight   int
	zones          []Zone
	particles      []*Particle
	audioCtx       *audio.Context
	audioStarted   bool
	asmr           *asmrEngine
	player         *audio.Player
	scores         []int
	isTouching     bool
	showLeaderboard bool
	currentZonePower float64
	fingerX, fingerY float64
	fingerSpeed    float64
}

func (g *Game) Update() error {
	g.time += 1.0 / 60.0
	touchIDs := ebiten.AppendTouchIDs(nil)
	g.isTouching = len(touchIDs) > 0 || ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)

	// Initialize audio on first user interaction
	if !g.audioStarted && g.isTouching { g.initAudio() }

	// Button Interaction Logic
	justTouched := false
	var tx, ty int
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		justTouched = true; tx, ty = ebiten.CursorPosition()
	} else {
		justIDs := inpututil.AppendJustPressedTouchIDs(nil)
		if len(justIDs) > 0 { justTouched = true; tx, ty = ebiten.TouchPosition(justIDs[0]) }
	}

	if justTouched {
		// Exit Button (Right)
		if tx > g.screenWidth-100 && ty < 60 { js.Global().Get("window").Get("location").Call("reload") }
		// Language Button (Left)
		if tx < 100 && ty < 60 { g.nextLang() }
		// Records Button (Center-Left)
		if tx > 110 && tx < 160 && ty < 60 { g.showLeaderboard = !g.showLeaderboard }
	}

	// INSTANT HARDWARE VOLUME CONTROL (Zero-latency buffer bypass)
	if g.player != nil {
		vol := 0.0
		if g.isTouching || g.win {
			vol = g.syncLevel
			if g.win { vol = 1.0 }
			if vol < 0.1 && (g.isTouching || g.win) { vol = 0.1 }
		}
		g.player.SetVolume(vol)
	}

	if g.win {
		g.winTime++
		if g.winTime == 1 { g.saveScore() }
		if g.winTime > 450 { g.nextLevel() }
		return nil
	}

	// Friction and Zone Logic
	friction := 0.0
	g.currentZonePower = 0.0
	g.fingerSpeed = 0
	if g.isTouching {
		var curX, curY int
		if len(touchIDs) > 0 { curX, curY = ebiten.TouchPosition(touchIDs[0]) } else { curX, curY = ebiten.CursorPosition() }
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
				if g.currentZonePower != 0 { mul = math.Abs(g.currentZonePower) }
				if g.currentZonePower < 0 { friction = -distMoved * 0.0005 } else { friction = distMoved * 0.00015 * mul }
			}
		}
		g.lastX, g.lastY = int(g.fingerX), int(g.fingerY)
	} else {
		g.lastX, g.lastY = 0, 0
		g.syncLevel -= 0.05 // Rapid drain
	}

	g.syncLevel += friction + (g.motionVal * 0.005)
	if g.syncLevel < 0 { g.syncLevel = 0 }
	if g.syncLevel > 1.0 { g.syncLevel = 1.0; g.win = true }

	// Move zones based on level difficulty
	for i := range g.zones {
		z := &g.zones[i]
		if z.power > 0 && g.level < 6 { continue }
		z.x += z.vx; z.y += z.vy
		if z.x < 50 || z.x > float64(g.screenWidth-50) { z.vx *= -1 }
		if z.y < 100 || z.y > float64(g.screenHeight-100) { z.vy *= -1 }
	}
	g.updateParticles()
	return nil
}

func (g *Game) nextLang() {
	for i, l := range langOrder { if l == g.lang { g.lang = langOrder[(i+1)%len(langOrder)]; return } }
}

func (g *Game) initAudio() {
	if g.audioCtx == nil { g.audioCtx = audio.NewContext(sampleRate) }
	if g.audioCtx != nil && !g.audioStarted {
		g.asmr = &asmrEngine{game: g}
		var err error
		g.player, err = g.audioCtx.NewPlayer(g.asmr)
		if err == nil {
			g.player.SetVolume(0)
			g.player.Play()
			g.audioStarted = true
		}
	}
}

func (g *Game) nextLevel() { g.level++; g.win = false; g.winTime = 0; g.syncLevel = 0; g.initZones() }

func (g *Game) initZones() {
	rand.Seed(time.Now().UnixNano())
	g.zones = nil
	speed := 1.0 + float64(g.level)*0.2
	for i := 0; i < 3; i++ {
		vx, vy := 0.0, 0.0
		if g.level >= 6 { vx = (rand.Float64()-0.5)*speed; vy = (rand.Float64()-0.5)*speed }
		g.zones = append(g.zones, Zone{x: float64(rand.Intn(g.screenWidth-100)+50), y: float64(rand.Intn(g.screenHeight-200)+100), vx: vx, vy: vy, power: 1.6 + float64(g.level)*0.1, radius: 110.0 - float64(g.level)*5.0})
	}
	numNeg := 1 + g.level/2
	if numNeg > 5 { numNeg = 5 }
	for i := 0; i < numNeg; i++ {
		g.zones = append(g.zones, Zone{x: float64(rand.Intn(g.screenWidth-100)+50), y: float64(rand.Intn(g.screenHeight-200)+100), vx: (rand.Float64()-0.5)*(speed+1.5), vy: (rand.Float64()-0.5)*(speed+1.5), power: -4.0, radius: 80})
	}
	g.loadScores()
}

func (g *Game) loadScores() {
	raw := js.Global().Get("localStorage").Call("getItem", "cyber_scores").String()
	var scs []int
	if raw != "null" && raw != "" { for _, s := range strings.Split(raw, ",") { if val, err := strconv.Atoi(s); err == nil { scs = append(scs, val) } } }
	g.scores = scs
}

func (g *Game) saveScore() {
	g.loadScores(); g.scores = append(g.scores, g.level+1); sort.Sort(sort.Reverse(sort.IntSlice(g.scores)))
	if len(g.scores) > 5 { g.scores = g.scores[:5] }
	var strScores []string
	for _, s := range g.scores { strScores = append(strScores, strconv.Itoa(s)) }
	js.Global().Get("localStorage").Call("setItem", "cyber_scores", strings.Join(strScores, ","))
}

func (g *Game) spawnParticles(x, y float64, n int) {
	for i := 0; i < n; i++ { g.particles = append(g.particles, &Particle{x: x, y: y, vx: (rand.Float64()-0.5)*5, vy: (rand.Float64()-0.5)*5, life: 1.0}) }
}

func (g *Game) updateParticles() {
	for i := len(g.particles)-1; i >= 0; i-- { p := g.particles[i]; p.x += p.vx; p.y += p.vy; p.life -= 0.04; if p.life <= 0 { g.particles = append(g.particles[:i], g.particles[i+1:]...) } }
}

// --- Deep Organic ASMR Engine ---
type asmrEngine struct {
	game      *Game
	lastBrown float64 
	lpFilterL, lpFilterR float64 
	breath    float64
}

func (a *asmrEngine) Read(p []byte) (int, error) {
	for i := 0; i < len(p)/4; i++ {
		// Generate constant sound (volume is hardware-controlled in Update)
		white := (rand.Float64()*2 - 1)
		
		// 1. Brownian Noise (Deep Ocean Texture)
		a.lastBrown = (a.lastBrown + (0.01 * white)) / 1.002
		base := a.lastBrown * 0.4
		
		// 2. Soft Breath Modulation
		a.breath += 0.0004
		breathMod := (math.Sin(a.breath) + 1.0) / 2.0
		whisper := white * 0.15 * breathMod
		
		// 3. Binaural Panning (Based on X finger position)
		pan := a.game.fingerX / float64(a.game.screenWidth)
		if pan < 0 { pan = 0 }; if pan > 1 { pan = 1 }
		
		raw := base + whisper
		a.lpFilterL = a.lpFilterL + 0.05*(raw-a.lpFilterL)
		a.lpFilterR = a.lpFilterR + 0.05*(raw-a.lpFilterR)
		
		sl := int16(a.lpFilterL * (1.0 - pan) * 32767)
		sr := int16(a.lpFilterR * pan * 32767)
		
		p[4*i], p[4*i+1], p[4*i+2], p[4*i+3] = byte(sl), byte(sl>>8), byte(sr), byte(sr>>8)
	}
	return len(p), nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	if g.screenWidth == 0 { g.screenWidth, g.screenHeight = w, h; g.initZones() }

	if g.shader != nil {
		op := &ebiten.DrawRectShaderOptions{}
		zoneType := 0.0
		ids := ebiten.AppendTouchIDs(nil)
		if len(ids) > 0 {
			tx, ty := ebiten.TouchPosition(ids[0])
			for _, z := range g.zones { if math.Sqrt(math.Pow(float64(tx)-z.x, 2) + math.Pow(float64(ty)-z.y, 2)) < z.radius { zoneType = z.power; break } }
		}
		op.Uniforms = map[string]interface{}{"Time": float32(g.time), "Cursor": []float32{float32(g.syncLevel), 0}, "WinTime": float32(g.winTime), "ZoneType": float32(zoneType)}
		screen.DrawRectShader(w, h, g.shader, op)
	}

	for _, p := range g.particles { vector.DrawFilledCircle(screen, float32(p.x), float32(p.y), 2*float32(p.life), color.RGBA{255, 255, 255, 180}, true) }

	dict := translations[g.lang]
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf(">> %s: %d", dict["lvl"], g.level+1), 20, 75)

	if g.win || g.showLeaderboard {
		// Leaderboard / Win Overlay
		vector.DrawFilledRect(screen, 40, float32(h/2-100), float32(w-80), 220, color.RGBA{0, 0, 0, 220}, true)
		title := dict["best"]; if g.win { title = dict["win"] }
		ebitenutil.DebugPrintAt(screen, title, w/2-60, h/2-80)
		for i, s := range g.scores { ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%d. %s %d", i+1, dict["lvl"], s), w/2-50, h/2-40+i*20) }
	} else {
		barW := float32(w-60); vector.DrawFilledRect(screen, 30, float32(h-40), barW, 2, color.RGBA{40, 40, 40, 150}, true)
		vector.DrawFilledRect(screen, 30, float32(h-40), barW * float32(g.syncLevel), 2, color.RGBA{255, 255, 255, 220}, true)
	}

	// HEADER BUTTONS
	// EXIT (Red)
	vector.DrawFilledRect(screen, float32(w-80), 10, 70, 40, color.RGBA{180, 0, 50, 200}, true)
	ebitenutil.DebugPrintAt(screen, dict["exit"], w-70, 25)
	// LANGUAGE (Blue)
	vector.DrawFilledRect(screen, 10, 10, 90, 40, color.RGBA{0, 100, 200, 200}, true)
	ebitenutil.DebugPrintAt(screen, dict["lang"], 20, 25)
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
	if err != nil { panic(err) }
	game := &Game{lang: "es", shader: s}
	ebiten.SetWindowSize(400, 800)
	if err := ebiten.RunGame(game); err != nil { panic(err) }
}
