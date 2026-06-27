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
//   - Service (tts.go) — the facade: Stream / Batch / Status (ready → processing
//     → speaking → ready). This is what ../voices binds to.
//
// Synthesis is a dumb speaker: it produces PCM; playback policy (queueing,
// barge-in/interrupt on VAD TRUE) lives in the consumer.
package tts
