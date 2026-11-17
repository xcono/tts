package tts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/xcono/tts/pkg/audio"
	"github.com/xcono/tts/pkg/config"
)

func TestClientSynthesizeDecodesStreamedPCM(t *testing.T) {
	want := []int16{100, -200, 300, -400, 32767, -32768}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(audio.PCM16LE(want))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1"}
	got, err := c.Synthesize(context.Background(), "привет")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestClientSynthesizeRequestShape(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Input          string `json:"input"`
		Voice          string `json:"voice"`
		ResponseFormat string `json:"response_format"`
		Model          string `json:"model"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write(audio.PCM16LE([]int16{1}))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1", Voice: "stas", Model: "tts-1"}
	if _, err := c.Synthesize(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/audio/speech" {
		t.Errorf("path = %q, want /v1/audio/speech", gotPath)
	}
	if gotBody.Input != "hi." {
		t.Errorf("input = %q, want %q (final stop added)", gotBody.Input, "hi.")
	}
	if gotBody.Voice != "stas" {
		t.Errorf("voice = %q, want stas", gotBody.Voice)
	}
	if gotBody.ResponseFormat != "pcm" {
		t.Errorf("response_format = %q, want pcm", gotBody.ResponseFormat)
	}
	if gotBody.Model != "tts-1" {
		t.Errorf("model = %q, want tts-1", gotBody.Model)
	}
}

func TestSynthesizeStreamDeliversChunksIncrementally(t *testing.T) {
	// The server sends the first chunk, then BLOCKS until the client confirms it
	// received that chunk, then sends the second. A client that buffered the whole
	// body (io.ReadAll) would never call emit, so the server would block forever
	// and this test would time out — proving the client streams.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		fl := w.(http.Flusher)
		_, _ = w.Write(audio.PCM16LE([]int16{1, 2}))
		fl.Flush()
		<-release // wait until the client has the first chunk
		_, _ = w.Write(audio.PCM16LE([]int16{3, 4}))
		fl.Flush()
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1"}
	var got []int16
	n := 0
	done := make(chan error, 1)
	go func() {
		done <- c.SynthesizeStream(context.Background(), "привет", func(chunk []int16) error {
			got = append(got, chunk...)
			n++
			if n == 1 {
				close(release) // got the first chunk → let the server send the second
			}
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SynthesizeStream: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out — client buffered the whole body instead of streaming")
	}

	if !reflect.DeepEqual(got, []int16{1, 2, 3, 4}) {
		t.Fatalf("got %v, want [1 2 3 4]", got)
	}
	if n < 2 {
		t.Fatalf("emit called %d times, want >=2 (chunks delivered as they arrive)", n)
	}
}

func TestSynthesizeStreamStopsOnEmitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(audio.PCM16LE([]int16{1, 2, 3, 4}))
	}))
	defer srv.Close()

	want := errors.New("sink closed")
	c := &Client{BaseURL: srv.URL + "/v1"}
	err := c.SynthesizeStream(context.Background(), "hi", func(chunk []int16) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestSynthesizeStreamErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1"}
	called := false
	err := c.SynthesizeStream(context.Background(), "hi", func([]int16) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected error for non-2xx status, got nil")
	}
	if called {
		t.Error("emit must not be called on an error status")
	}
}

func TestClientSynthesizeErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1"}
	if _, err := c.Synthesize(context.Background(), "hi"); err == nil {
		t.Fatal("expected error for non-2xx status, got nil")
	}
}

// A zero-value voice setup must not leak empty vLLM-Omni fields into the body:
// a server with its own defaults has to keep working.
func TestClientOmitsUnsetVoiceFields(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write(audio.PCM16LE([]int16{1}))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1"}
	if _, err := c.Synthesize(context.Background(), "Привет."); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"language", "ref_audio", "ref_text", "extra_params", "voice", "model"} {
		if _, ok := raw[k]; ok {
			t.Errorf("unset field %q was sent: %v", k, raw[k])
		}
	}
}

func TestClientSendsVoiceCloneFields(t *testing.T) {
	var got struct {
		Language    string `json:"language"`
		RefAudio    string `json:"ref_audio"`
		RefText     string `json:"ref_text"`
		ExtraParams struct {
			Steps    int     `json:"num_inference_steps"`
			Guidance float64 `json:"guidance_scale"`
		} `json:"extra_params"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write(audio.PCM16LE([]int16{1}))
	}))
	defer srv.Close()

	c := NewClient(config.Qwen3Config{
		BaseURL:  srv.URL + "/v1",
		Language: "ru",
		RefAudio: "file:///ref/ref_stas.wav",
		RefText:  "Это нелегко.",
		Steps:    32,
		Guidance: 2.0,
	})
	if _, err := c.Synthesize(context.Background(), "Привет."); err != nil {
		t.Fatal(err)
	}
	if got.Language != "ru" || got.RefAudio != "file:///ref/ref_stas.wav" || got.RefText != "Это нелегко." {
		t.Errorf("voice fields = %+v", got)
	}
	if got.ExtraParams.Steps != 32 || got.ExtraParams.Guidance != 2.0 {
		t.Errorf("extra_params = %+v, want 32 steps, guidance 2", got.ExtraParams)
	}
}

func TestFinalStop(t *testing.T) {
	for in, want := range map[string]string{
		"Тесты прошли":          "Тесты прошли.",
		"Тесты прошли.":         "Тесты прошли.",
		"Готово!":               "Готово!",
		"Правда?":               "Правда?",
		"И так далее…":          "И так далее…",
		"Он сказал «готово.»":   "Он сказал «готово.»",
		"Он сказал «готово»":    "Он сказал «готово».",
		"  ветка main, коммит ": "ветка main, коммит.",
		"":                      "",
	} {
		if got := finalStop(in); got != want {
			t.Errorf("finalStop(%q) = %q, want %q", in, got, want)
		}
	}
}

// An empty config section must speak with the project's voice: voices builds the
// client from its own config and sets only the URL.
func TestNewClientFillsDefaults(t *testing.T) {
	c := NewClient(config.Qwen3Config{BaseURL: "http://tts:8007/v1"})
	if c.BaseURL != "http://tts:8007/v1" {
		t.Errorf("BaseURL = %q, want the configured one", c.BaseURL)
	}
	if c.Language != config.DefaultLanguage || c.Steps != config.DefaultSteps || c.Guidance != config.DefaultGuidance {
		t.Errorf("sampler defaults not applied: %+v", c)
	}
	if c.RefAudio != config.DefaultRefAudio || c.RefText != config.DefaultRefText {
		t.Errorf("reference defaults not applied: %q / %q", c.RefAudio, c.RefText)
	}
}

// A named voice selects one stored on the server; the default reference clip
// must not override it.
func TestNewClientKeepsNamedVoice(t *testing.T) {
	c := NewClient(config.Qwen3Config{Voice: "stas"})
	if c.RefAudio != "" || c.RefText != "" {
		t.Errorf("reference sent alongside a named voice: %q / %q", c.RefAudio, c.RefText)
	}
}
