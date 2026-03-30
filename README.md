# Cyber Euphoria

An immersive sensory experience optimized for mobile browsers. Using voice synthesis and custom shaders, Cyber Euphoria guides you into a state of total digital synchrony through rhythmic touch and motion.

## Core Mechanics

- **Rhythmic Friction:** Charge the digital core by moving your finger across the screen. Synergy drops rapidly when you stop or lift your finger.
- **Dynamic Sensory Zones:**
  - **Pleasure Nodes:** Hidden areas that trigger haptic feedback, audio response, and accelerated energy gain. Combo multiplier builds while staying in these zones.
  - **Interference Zones:** Turbulent areas that tint the core red and drain your synchrony.
- **Timer:** Each level has a countdown (25 s at level 1, decreasing to a 15 s floor). Run out of time and it's game over.
- **Progressive Difficulty:**
  - Negative zone count scales with level (up to 6).
  - From **level 3** onwards, pleasure zones become mobile.
  - Zone radius shrinks and speed increases each level.

## Audio Engine

- **Voice Synthesis:** Harmonic sine series (5 partials) tuned to female vocal timbre, rising in pitch and pace with sync level. Win state triggers a rapid climax arc. Breath rate starts at 0.3 Hz (meditative) and rises to 1.8 Hz at full sync.
- **Zero-Latency Hardware Control:** Volume is set at the hardware level each frame — sound stops the instant you lift your finger.
- **Binaural Spatial Audio:** Sound pans between L/R channels based on finger X position.
- **Binaural Beat Entrainment:** Right channel runs at baseFreq + 10 Hz, creating a 10 Hz alpha-wave phantom beat between ears (best with headphones).
- **Ambient Drone:** Constant 60 Hz sub-bass hum at low volume — establishes a meditative space even between touches.
- **Touch-Start Burst:** 20 ms filtered noise click on first contact each touch — an ASMR trigger.
- **Vibrato & Breath Envelope:** 5.5 Hz vibrato LFO and breath-rhythm modulation for natural inflection.
- **Low-Pass Filtering:** One-pole IIR filter (800–1600 Hz, scales with sync) smooths out high-frequency buzz.

## Features

- **Mobile Optimized:** Large touch targets, WebAssembly logic, haptic feedback via `navigator.vibrate`.
- **Finger Trail:** Glowing trail follows the finger — color shifts green in pleasure zones, red in interference zones.
- **Combo Pop:** Multiplier value (e.g. `2.4×`) floats near the finger when the combo builds, making the scoring system visible.
- **Personal Best:** Best level tracked in `localStorage` — shown on every game-over screen with new-record detection.
- **"Almost" Encouragement:** When ≤5 s remain and sync ≥70%, a motivational message appears.
- **Server Leaderboard:** Top 5 scores stored server-side (`GET/POST leaderboard.php`).
- **Milestone Popups:** Expanding ring flash at 25 / 50 / 75 % sync with haptic feedback.
- **Multi-language Support:** EN, ES, FR, RU (Russian transliterated).

## Build & Run

### Requirements

- [Go](https://go.dev/) 1.21+
- [Ebitengine](https://ebitengine.org/) dependencies

### Build WASM

```bash
./rebuild.sh
# equivalent: GOOS=js GOARCH=wasm go build -o main.wasm main.go
```

### Local Development

```bash
go run server.go
# Visit http://localhost:8080 — use mobile device on same network for touch testing
```

### Production (Apache/Hostinger)

Upload: `index.html`, `main.wasm`, `wasm_exec.js`, `.htaccess`

The `.htaccess` sets the `application/wasm` MIME type and cache headers.

## Technical Stack

- **Logic:** Go (WASM)
- **Graphics:** Ebitengine + Kage shaders
- **Audio:** Custom harmonic voice synthesizer
- **Backend:** PHP leaderboard endpoint (`leaderboard.php`)
