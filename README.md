# Cyber Euphoria 🌌

An immersive **Digital ASMR & Sensory Satisfaction** experience optimized for mobile browsers. Using advanced acoustic synthesis and custom shaders, Cyber Euphoria guides you into a state of total digital synchrony through rhythmic touch and motion.

## 🕹️ Core Mechanics
- **Rhythmic Friction (Rubbing):** Charge the digital core by stroking the screen. Synergy levels are volatile and will drop rapidly if physical interaction ceases.
- **Dynamic Sensory Zones:**
  - **Pleasure Nodes:** Hidden areas that trigger haptic feedback, soft breathing sounds, and accelerated energy gain.
  - **Interference Zones:** Turbulent areas that tint the core red and violently drain your synchrony.
- **Progressive Evolution:** 
  - Difficulty increases every level. 
  - From **Level 6** onwards, pleasure zones become mobile, requiring you to actively track them across the screen.

## 🎧 Advanced ASMR Engine
- **Zero-Latency Hardware Control:** Unlike standard web audio, volume is managed at the hardware trigger level, ensuring the sound stops the millisecond you lift your finger.
- **Deep Brownian Texture:** Replaces harsh static with organic, deep-frequency noise (Brownian Noise), creating a sensation similar to ocean waves or soft wind.
- **Binaural Spatial Audio:** Sound moves dynamically between Left and Right channels based on your finger's position, creating a 3D sense of physical proximity.
- **Resonant Breath:** Modulated "whisper" effects that pulse rhythmically when interacting with pleasure nodes.

## 🏆 Features
- **Mobile Optimized:** Large touch targets and high-performance WebAssembly logic.
- **Local Leaderboard:** Saves your top 5 highest levels reached on your device.
- **Multi-language Support:** Integrated toggle for English (EN), Spanish (ES), and Russian (RU - Transliterated).

## 🚀 Deployment & Installation

### Requirements
- [Go](https://go.dev/) (1.24+ recommended)
- [Ebitengine](https://ebitengine.org/) dependencies.

### Compilation
Build the WebAssembly binary:
```bash
./rebuild.sh
```

### Local Development
Run the development server to handle WASM MIME types correctly:
```bash
go run server.go
```
Visit `http://localhost:8080` on your mobile device (ensure you are on the same network).

### Production (Hostinger/Apache)
Upload these files to your server:
- `index.html`
- `main.wasm`
- `wasm_exec.js`
- `.htaccess` (Ensures correct MIME types and prevents cache issues)

## 🛠️ Technical Stack
- **Logic:** Go (WASM)
- **Graphics:** Ebitengine + Kage (GLSL-like Shaders)
- **Audio:** Custom Granular & Brownian Oscillator

---
*Experience digital intimacy through high-fidelity acoustics and responsive design.*
