package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xcono/tts/pkg/audio"
)

// SampleRate is the TTS wire format's fixed output rate: 24 kHz mono 16-bit LE.
const SampleRate = 24000

// Client posts text to the local Qwen3-TTS server's OpenAI-compatible
// POST /v1/audio/speech endpoint and returns synthesised PCM samples.
type Client struct {
	BaseURL string       // e.g. "http://localhost:8007/v1"
	Voice   string       // voice name; "" lets the server pick its default
	Model   string       // optional; sent as the "model" field
	HTTP    *http.Client // nil => http.DefaultClient
}

// speechRequest mirrors the server's SpeechRequest pydantic model.
type speechRequest struct {
	Model          string `json:"model,omitempty"`
	Input          string `json:"input"`
	Voice          string `json:"voice,omitempty"`
	ResponseFormat string `json:"response_format"`
}

// post sends the speech request and returns the open response. The caller must
// close resp.Body. A non-2xx status is turned into an error here.
func (c *Client) post(ctx context.Context, text string) (*http.Response, error) {
	body, err := json.Marshal(speechRequest{
		Model:          c.Model,
		Input:          text,
		Voice:          c.Voice,
		ResponseFormat: "pcm", // raw int16 LE chunks, no header
	})
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/audio/speech"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("tts: status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return resp, nil
}

// SynthesizeStream requests speech for text and calls emit with each PCM chunk
// as it arrives off the wire — so playback can start at first audio instead of
// waiting for the whole clip. The server streams raw int16 LE PCM with no
// header. emit's slice is freshly allocated and may be retained. If emit returns
// an error, streaming stops and that error is returned.
func (c *Client) SynthesizeStream(ctx context.Context, text string, emit func(chunk []int16) error) error {
	resp, err := c.post(ctx, text)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return streamPCM16LE(resp.Body, emit, "tts")
}

// Synthesize requests speech for text and returns the full 24 kHz mono PCM clip,
// accumulating the streamed chunks. Use SynthesizeStream for play-as-you-go.
func (c *Client) Synthesize(ctx context.Context, text string) ([]int16, error) {
	return accumulate(ctx, c, text)
}

// streamPCM16LE decodes a raw int16-LE PCM stream, handing each chunk to emit as
// it arrives and carrying a sample split across reads (odd leftover byte) into
// the next read. Stops and returns emit's error if it returns one. who labels
// read errors for the calling backend.
func streamPCM16LE(r io.Reader, emit func(chunk []int16) error, who string) error {
	buf := make([]byte, 8192)
	var carry []byte
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			data := buf[:n]
			if len(carry) > 0 {
				data = append(carry, data...)
				carry = carry[:0]
			}
			if len(data)%2 == 1 {
				carry = append(carry, data[len(data)-1])
				data = data[:len(data)-1]
			}
			if len(data) > 0 {
				if err := emit(audio.DecodePCM16LE(data)); err != nil {
					return err
				}
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("%s: read stream: %w", who, readErr)
		}
	}
}

// accumulate runs a Synthesizer's stream into the full clip — the shared body of
// every Synthesize, so the one-shot path always matches the streaming one.
func accumulate(ctx context.Context, s Synthesizer, text string) ([]int16, error) {
	out := make([]int16, 0)
	err := s.SynthesizeStream(ctx, text, func(chunk []int16) error {
		out = append(out, chunk...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
