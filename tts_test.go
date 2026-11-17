package tts

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xcono/tts/pkg/config"
)

// fakeProvider satisfies Provider with scripted chunks. before, if set, runs at
// the start of SynthesizeStream (so a test can observe Status == Processing
// before the first chunk).
type fakeProvider struct {
	chunks [][]int16
	err    error
	before func()
}

func (f fakeProvider) SynthesizeStream(_ context.Context, _ string, emit func([]int16) error) error {
	if f.before != nil {
		f.before()
	}
	for _, c := range f.chunks {
		if e := emit(c); e != nil {
			return e
		}
	}
	return f.err
}

func (f fakeProvider) Synthesize(ctx context.Context, text string) ([]int16, error) {
	return accumulate(ctx, f, text)
}

func TestServiceStatusLifecycle(t *testing.T) {
	var svc *Service
	var atStart Status

	p := fakeProvider{
		chunks: [][]int16{{1, 2}, {3, 4}},
		before: func() { atStart = svc.Status() }, // before any chunk
	}
	svc = NewWith(p)

	if svc.Status() != Ready {
		t.Fatalf("initial status = %v, want ready", svc.Status())
	}

	got, err := svc.Batch(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}

	if atStart != Processing {
		t.Errorf("status before first chunk = %v, want processing", atStart)
	}
	if !reflect.DeepEqual(got, []int16{1, 2, 3, 4}) {
		t.Errorf("clip = %v, want [1 2 3 4]", got)
	}
	if svc.Status() != Ready {
		t.Errorf("status after completion = %v, want ready", svc.Status())
	}
}

func TestServiceSpeakingDuringStream(t *testing.T) {
	svc := NewWith(fakeProvider{chunks: [][]int16{{1}, {2}}})

	var seen []Status
	err := svc.Stream(context.Background(), "hi", func(chunk []int16) error {
		seen = append(seen, svc.Status())
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for i, s := range seen {
		if s != Speaking {
			t.Errorf("status at emit %d = %v, want speaking", i, s)
		}
	}
	if svc.Status() != Ready {
		t.Errorf("status after stream = %v, want ready", svc.Status())
	}
}

func TestServiceStatusResetsAfterError(t *testing.T) {
	wantErr := errors.New("backend down")
	svc := NewWith(fakeProvider{err: wantErr})

	_, err := svc.Batch(context.Background(), "hi")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if svc.Status() != Ready {
		t.Errorf("status after error = %v, want ready", svc.Status())
	}
}

func TestServiceUnknownProvider(t *testing.T) {
	if _, err := provider(config.TTSConfig{Provider: "bogus"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewBuildsConfiguredProvider(t *testing.T) {
	// "fallback" wires ElevenLabs primary + local secondary; just assert New
	// accepts it and returns a usable Service (no network call here).
	svc, err := New(&config.Config{TTS: config.TTSConfig{Provider: "fallback"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.Status() != Ready {
		t.Errorf("fresh service status = %v, want ready", svc.Status())
	}
}
