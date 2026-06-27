package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultElevenBaseURL = "https://api.elevenlabs.io"
	defaultElevenModel   = "eleven_flash_v2_5" // ~75 ms model latency, multilingual
)

// sharedHTTP is a keep-alive client reused across ElevenLabs requests so the TLS
// connection to the (geo-routed) edge stays warm — the gap between the ~175 ms
// warm TTFA and a ~1.3 s cold handshake. Reusing one client across turns is what
// makes the warm number real.
var sharedHTTP = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// ElevenLabs streams speech from ElevenLabs' HTTP /stream endpoint as raw PCM.
// We request pcm_<rate> (default pcm_24000 = 24 kHz mono 16-bit LE) — the TTS
// wire format, headerless, so it decodes exactly like the local server's stream.
// It satisfies the same streaming contract as *Client, so it drops into the
// play-as-you-go loop unchanged.
//
// Note: PCM output formats may require a paid ElevenLabs plan; on a plan that
// rejects PCM, fall back to local (see Fallback). The warm ~175 ms TTFA needs a
// reused client — leave HTTP nil to use the shared keep-alive client, or pass
// your own pooled one.
type ElevenLabs struct {
	APIKey     string       // xi-api-key
	VoiceID    string       // ElevenLabs voice_id
	Model      string       // model_id; "" => eleven_flash_v2_5
	BaseURL    string       // "" => https://api.elevenlabs.io
	SampleRate int          // 0 => 24000; sent as output_format=pcm_<rate>
	HTTP       *http.Client // nil => shared keep-alive client
}

// elevenRequest is the minimal /stream body. Voice settings are left to the
// voice/model defaults — flash needs none for a latency benchmark-grade turn.
type elevenRequest struct {
	Text    string `json:"text"`
	ModelID string `json:"model_id"`
}

func (c *ElevenLabs) rate() int {
	if c.SampleRate == 0 {
		return SampleRate // 24000
	}
	return c.SampleRate
}

// post sends the request and returns the open response; caller closes Body. A
// non-2xx is turned into an error here, mirroring Client.post.
func (c *ElevenLabs) post(ctx context.Context, text string) (*http.Response, error) {
	model := c.Model
	if model == "" {
		model = defaultElevenModel
	}
	body, err := json.Marshal(elevenRequest{Text: text, ModelID: model})
	if err != nil {
		return nil, err
	}

	base := c.BaseURL
	if base == "" {
		base = defaultElevenBaseURL
	}
	url := fmt.Sprintf("%s/v1/text-to-speech/%s/stream?output_format=pcm_%d",
		strings.TrimRight(base, "/"), c.VoiceID, c.rate())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/pcm")
	req.Header.Set("xi-api-key", c.APIKey)

	client := c.HTTP
	if client == nil {
		client = sharedHTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("elevenlabs: status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return resp, nil
}

// SynthesizeStream requests speech and calls emit with each PCM chunk as it
// arrives — same contract as Client.SynthesizeStream.
func (c *ElevenLabs) SynthesizeStream(ctx context.Context, text string, emit func(chunk []int16) error) error {
	resp, err := c.post(ctx, text)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return streamPCM16LE(resp.Body, emit, "elevenlabs")
}

// Synthesize accumulates the streamed chunks into the full clip.
func (c *ElevenLabs) Synthesize(ctx context.Context, text string) ([]int16, error) {
	return accumulate(ctx, c, text)
}
