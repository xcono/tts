module github.com/xcono/tts

go 1.25.0

// oto is the speaker's output device (pkg/play). Pinned to the v3.5.0-alpha line
// because its cgo-free Linux rewrite (pure-Go PulseAudio + purego-loaded ALSA)
// lands there — stable v3.4.0 still uses a #cgo ALSA driver. Keeps the module
// cgo-free (CGO_ENABLED=0 builds). Bump to v3.5.0 once it ships.
require github.com/ebitengine/oto/v3 v3.5.0-alpha.8

require (
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/jfreymuth/pulse v0.1.1 // indirect
	golang.org/x/sys v0.45.0 // indirect
)
