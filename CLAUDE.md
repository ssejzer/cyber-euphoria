# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build WASM binary (outputs main.wasm)
./rebuild.sh
# equivalent to: GOOS=js GOARCH=wasm go build -o main.wasm main.go

# Run local dev server (handles WASM MIME types + CORS headers)
go run server.go
# Visit http://localhost:8080 — use mobile device on same network for touch testing
```

There are no tests. There is no linter configuration.

## Architecture

The entire game is a single file: `main.go`, compiled to `main.wasm`. `server.go` is a standalone dev server, not part of the game build.

**Ebitengine game loop** — `Game` implements `ebiten.Game` via `Update()` (logic, 60 FPS) and `Draw()` (rendering). `Layout()` returns the native screen size unchanged.

**Rendering pipeline:**
1. Full-screen Kage shader (`shaderSource` constant) draws the animated neon core. Uniforms passed each frame: `Time` (animation clock), `Cursor.x` (repurposed as syncLevel 0–1), `WinTime` (win flash counter), `ZoneType` (positive/negative/neutral zone tint).
2. Particles drawn as plain circles via `vector.DrawFilledCircle`.
3. All UI text uses `ebitenutil.DebugPrintAt` (monospace pixel font, no custom fonts).

**Audio** — `voiceEngine` implements `io.Reader` and is fed directly into an Ebitengine audio player. Sound is generated sample-by-sample: harmonic sine synthesis (female voice timbre) + breath envelope modulation + vibrato LFO + airy noise + binaural panning (L/R split by finger X). Volume is set hardware-side via `player.SetVolume()` each frame — this achieves zero-latency mute on finger lift, bypassing the audio buffer.

**Zone system** — `zones []Zone` holds both pleasure nodes (power > 0) and interference zones (power = -5.0). Zones are regenerated in `initZones()` each level. Positive zones are stationary until level 3+, negative zones always move. Radius shrinks per level: `90 - level*4` px, minimum 35 px. Negative zone count scales with level: `2 + level/2`, capped at 6.

**Timer & game-over** — each level has a countdown (`levelDuration`: starts at 25 s, decreases by 1 s per level, floor 15 s). Reaching zero triggers game-over; score is saved and a retry prompt is shown.

**Combo system** — `comboCount` increments while moving in positive zones (max 60), decaying when touching negative zones or lifting the finger. Combo multiplier `1 + comboCount/60` scales sync gain.

**Milestones** — at 25 / 50 / 75 % sync a flash overlay and message appear for 90 frames; haptic feedback fired via `navigator.vibrate(50)`.

**Persistence** — scores are stored server-side. `loadScores()` calls `GET /leaderboard`; `saveScore()` calls `POST /leaderboard` with `{name, level}` JSON. Top 5 entries are cached in `g.scores`. Player name is persisted in `localStorage` under key `cyber_player_name` (prompted once, max 8 chars, uppercased).

**Localization** — `translations` map holds 4 languages (en/es/fr/ru). Russian is transliterated Latin to avoid font rendering issues with the debug font.

## Deployment

For production (Apache/Hostinger), upload: `index.html`, `main.wasm`, `wasm_exec.js`, `.htaccess`. The `.htaccess` sets the `application/wasm` MIME type and cache headers.
