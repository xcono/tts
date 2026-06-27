package tts

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// synthFunc adapts a function to the Synthesizer interface for tests.
type synthFunc func(ctx context.Context, text string, emit func(chunk []int16) error) error

func (f synthFunc) SynthesizeStream(ctx context.Context, text string, emit func(chunk []int16) error) error {
	return f(ctx, text, emit)
}

func emitAll(samples []int16) synthFunc {
	return func(_ context.Context, _ string, emit func([]int16) error) error {
		return emit(samples)
	}
}

func TestFallbackPrimarySuccessSkipsSecondary(t *testing.T) {
	secondaryCalled := false
	f := &Fallback{
		Primary:   emitAll([]int16{1, 2, 3}),
		Secondary: synthFunc(func(context.Context, string, func([]int16) error) error { secondaryCalled = true; return nil }),
	}
	got, err := f.Synthesize(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int16{1, 2, 3}) {
		t.Fatalf("got %v, want [1 2 3]", got)
	}
	if secondaryCalled {
		t.Error("secondary must not run when primary succeeds")
	}
}

func TestFallbackDegradesBeforeAudio(t *testing.T) {
	primaryErr := errors.New("cloud 503")
	fellBack := false
	f := &Fallback{
		Primary:    synthFunc(func(context.Context, string, func([]int16) error) error { return primaryErr }),
		Secondary:  emitAll([]int16{7, 8, 9}),
		OnFallback: func(err error) { fellBack = errors.Is(err, primaryErr) },
	}
	got, err := f.Synthesize(context.Background(), "hi")
	if err != nil {
		t.Fatalf("expected degrade to secondary, got err %v", err)
	}
	if !reflect.DeepEqual(got, []int16{7, 8, 9}) {
		t.Fatalf("got %v, want secondary audio [7 8 9]", got)
	}
	if !fellBack {
		t.Error("OnFallback should fire with the primary error")
	}
}

func TestFallbackNoDegradeAfterPrimaryEmitted(t *testing.T) {
	primaryErr := errors.New("dropped mid-stream")
	secondaryCalled := false
	f := &Fallback{
		Primary: synthFunc(func(_ context.Context, _ string, emit func([]int16) error) error {
			_ = emit([]int16{1, 2}) // already played → cannot restart
			return primaryErr
		}),
		Secondary: synthFunc(func(context.Context, string, func([]int16) error) error { secondaryCalled = true; return nil }),
	}
	err := f.SynthesizeStream(context.Background(), "hi", func([]int16) error { return nil })
	if err == nil {
		t.Fatal("expected mid-stream failure to surface, got nil")
	}
	if !errors.Is(err, primaryErr) {
		t.Errorf("err = %v, want it to wrap primaryErr", err)
	}
	if secondaryCalled {
		t.Error("must NOT fall back once audio has already played")
	}
}

func TestFallbackSurfacesEmitError(t *testing.T) {
	sinkErr := errors.New("sink closed")
	secondaryCalled := false
	f := &Fallback{
		Primary:   emitAll([]int16{1}),
		Secondary: synthFunc(func(context.Context, string, func([]int16) error) error { secondaryCalled = true; return nil }),
	}
	err := f.SynthesizeStream(context.Background(), "hi", func([]int16) error { return sinkErr })
	if !errors.Is(err, sinkErr) {
		t.Fatalf("err = %v, want sinkErr surfaced unchanged", err)
	}
	if secondaryCalled {
		t.Error("an emit/sink error must never trigger fallback")
	}
}
