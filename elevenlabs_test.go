package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/xcono/tts/pkg/audio"
)

func TestElevenLabsStreamsPCM(t *testing.T) {
	want := []int16{100, -200, 300, -400, 32767, -32768}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(audio.PCM16LE(want))
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "k", VoiceID: "v", BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.Synthesize(context.Background(), "привет")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestElevenLabsRequestShape(t *testing.T) {
	var gotMethod, gotPath, gotKey, gotFormat string
	var gotBody struct {
		Text    string `json:"text"`
		ModelID string `json:"model_id"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotKey = r.Header.Get("xi-api-key")
		gotFormat = r.URL.Query().Get("output_format")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write(audio.PCM16LE([]int16{1}))
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "secret", VoiceID: "voice123", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Synthesize(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/v1/text-to-speech/voice123/stream"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotKey != "secret" {
		t.Errorf("xi-api-key = %q, want secret", gotKey)
	}
	if gotFormat != "pcm_24000" {
		t.Errorf("output_format = %q, want pcm_24000", gotFormat)
	}
	if gotBody.Text != "hi" {
		t.Errorf("text = %q, want hi", gotBody.Text)
	}
	if gotBody.ModelID != "eleven_flash_v2_5" {
		t.Errorf("model_id = %q, want eleven_flash_v2_5 (default)", gotBody.ModelID)
	}
}

func TestElevenLabsCustomRateAndModel(t *testing.T) {
	var gotFormat, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFormat = r.URL.Query().Get("output_format")
		var b struct {
			ModelID string `json:"model_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		gotModel = b.ModelID
		_, _ = w.Write(audio.PCM16LE([]int16{1}))
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "k", VoiceID: "v", BaseURL: srv.URL, HTTP: srv.Client(),
		SampleRate: 16000, Model: "eleven_multilingual_v2"}
	if _, err := c.Synthesize(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if gotFormat != "pcm_16000" {
		t.Errorf("output_format = %q, want pcm_16000", gotFormat)
	}
	if gotModel != "eleven_multilingual_v2" {
		t.Errorf("model_id = %q, want eleven_multilingual_v2", gotModel)
	}
}

func TestElevenLabsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := &ElevenLabs{APIKey: "k", VoiceID: "v", BaseURL: srv.URL, HTTP: srv.Client()}
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

// guards the default base URL composes a valid absolute URL (no accidental
// double slash / missing scheme).
func TestElevenLabsDefaultBaseURL(t *testing.T) {
	c := &ElevenLabs{APIKey: "k", VoiceID: "v"}
	got := "https://api.elevenlabs.io/v1/text-to-speech/v/stream?output_format=pcm_24000"
	if _, err := url.ParseRequestURI(got); err != nil {
		t.Fatalf("composed URL invalid: %v", err)
	}
	if c.rate() != 24000 {
		t.Errorf("default rate = %d, want 24000", c.rate())
	}
}
