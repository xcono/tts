// Package tts is the importable text-to-speech module for a realtime voice
// agent. It is the TTS counterpart of github.com/xcono/stt: a small, in-process
// library the LLM loop in ../voices wires over Go channels/callbacks, with no
// NATS.
//
// The contract has three layers:
//
//   - Provider (provider.go) — the streaming + one-shot synthesis backends:
//     *Client (local Qwen3-TTS), *ElevenLabs (cloud), and *Fallback (degrade to
//     local). All emit 24 kHz mono int16 LE PCM (SampleRate).
//   - Service (tts.go) — the synthesis facade: Stream / Batch / Status (ready →
//     processing → speaking → ready). Produces PCM; it touches no audio device.
//   - Speaker (pkg/play) — the playback facade: it OWNS the output device (oto,
//     cgo-free) and turns reply text into sound, with a play-as-you-go timeline,
//     look-ahead synthesis, and chunk-granular barge-in. This is what ../voices
//     binds to for the speaking path; it drives the Speaker by text + Interrupt,
//     never by PCM.
//
// The whole module is cgo-free, including the speaker: pkg/play drives
// github.com/ebitengine/oto/v3, which on Linux uses pure-Go PulseAudio
// (github.com/jfreymuth/pulse) and falls back to ALSA loaded via purego — so it
// builds with CGO_ENABLED=0 and needs no audio dev headers. At runtime it needs
// a reachable PulseAudio server (set PULSE_SERVER if not auto-discovered) or,
// failing that, libasound.so.2 present for the ALSA fallback. (Capture stays on
// PortAudio in xcono/asr; only the speaker lives here.)
//
// Playback policy that needs cross-module state — barge-in on the ASR's VAD
// signal — still lives in the consumer: it calls Speaker.Interrupt.
package tts
