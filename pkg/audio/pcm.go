// Package audio is the TTS module's minimal audio layer: the PCM wire-format
// codec. Copied from the stt/voices audio package (per the decoupling plan,
// shared audio is duplicated, not centralised, until drift forces a shared
// module). TTS only needs PCM16LE encode/decode — no capture, no resample.
package audio

import "encoding/binary"

// PCM16LE serialises int16 samples to little-endian bytes — the raw wire format
// the TTS servers stream.
func PCM16LE(samples []int16) []byte {
	b := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(s))
	}
	return b
}

// DecodePCM16LE deserialises little-endian 16-bit PCM bytes back to int16
// samples — the inverse of PCM16LE, used to decode the raw chunks the TTS server
// streams. A dangling trailing byte (a sample split across stream reads) is
// dropped rather than panicked on.
func DecodePCM16LE(b []byte) []int16 {
	samples := make([]int16, len(b)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return samples
}
