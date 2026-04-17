package pipeline

import (
	"context"
	"errors"
	"testing"
)

type counterState struct {
	Calls  int
	Marker string
}

type incProcessor struct {
	tag string
}

func (p incProcessor) Run(_ context.Context, s *counterState) error {
	s.Calls++
	s.Marker += p.tag
	return nil
}

type failProcessor struct {
	err error
}

func (p failProcessor) Run(_ context.Context, _ *counterState) error {
	return p.err
}

func TestExecute(t *testing.T) {
	t.Run("runs all steps in order", func(t *testing.T) {
		state := &counterState{}
		err := Execute(context.Background(), state, []Processor[counterState]{
			incProcessor{tag: "a"},
			incProcessor{tag: "b"},
			incProcessor{tag: "c"},
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if state.Calls != 3 {
			t.Errorf("Calls = %d, want 3", state.Calls)
		}
		if state.Marker != "abc" {
			t.Errorf("Marker = %q, want abc", state.Marker)
		}
	})

	t.Run("short-circuits on error", func(t *testing.T) {
		state := &counterState{}
		boom := errors.New("boom")
		err := Execute(context.Background(), state, []Processor[counterState]{
			incProcessor{tag: "a"},
			failProcessor{err: boom},
			incProcessor{tag: "c"},
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
		if state.Calls != 1 {
			t.Errorf("Calls = %d, want 1 (third step must not run)", state.Calls)
		}
	})

	t.Run("propagates context cancel before next step", func(t *testing.T) {
		state := &counterState{}
		ctx, cancel := context.WithCancel(context.Background())
		err := Execute(ctx, state, []Processor[counterState]{
			ProcessorFunc[counterState](func(_ context.Context, s *counterState) error {
				s.Calls++
				cancel()
				return nil
			}),
			incProcessor{tag: "never"},
		})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if state.Calls != 1 {
			t.Errorf("Calls = %d, want 1 (second step must not run)", state.Calls)
		}
	})
}

func TestLoopProcessor(t *testing.T) {
	t.Run("stops when Until returns true", func(t *testing.T) {
		state := &counterState{}
		loop := LoopProcessor[counterState]{
			Max:   10,
			Until: func(s *counterState) bool { return s.Calls >= 3 },
			Body: []Processor[counterState]{
				incProcessor{tag: "x"},
			},
		}
		if err := loop.Run(context.Background(), state); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if state.Calls != 3 {
			t.Errorf("Calls = %d, want 3", state.Calls)
		}
	})

	t.Run("returns ErrLoopMax when Until never true", func(t *testing.T) {
		state := &counterState{}
		loop := LoopProcessor[counterState]{
			Max:   4,
			Until: func(_ *counterState) bool { return false },
			Body: []Processor[counterState]{
				incProcessor{tag: "x"},
			},
		}
		err := loop.Run(context.Background(), state)
		if !errors.Is(err, ErrLoopMax) {
			t.Errorf("err = %v, want ErrLoopMax", err)
		}
		if state.Calls != 4 {
			t.Errorf("Calls = %d, want 4", state.Calls)
		}
	})

	t.Run("body error propagates", func(t *testing.T) {
		state := &counterState{}
		boom := errors.New("body boom")
		loop := LoopProcessor[counterState]{
			Max:   5,
			Until: func(_ *counterState) bool { return false },
			Body: []Processor[counterState]{
				incProcessor{tag: "x"},
				failProcessor{err: boom},
			},
		}
		err := loop.Run(context.Background(), state)
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want boom", err)
		}
		if state.Calls != 1 {
			t.Errorf("Calls = %d, want 1", state.Calls)
		}
	})

	t.Run("honors context cancel mid-loop", func(t *testing.T) {
		state := &counterState{}
		ctx, cancel := context.WithCancel(context.Background())
		loop := LoopProcessor[counterState]{
			Max:   10,
			Until: func(_ *counterState) bool { return false },
			Body: []Processor[counterState]{
				ProcessorFunc[counterState](func(_ context.Context, s *counterState) error {
					s.Calls++
					if s.Calls == 2 {
						cancel()
					}
					return nil
				}),
			},
		}
		err := loop.Run(ctx, state)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if state.Calls != 2 {
			t.Errorf("Calls = %d, want 2", state.Calls)
		}
	})

	t.Run("rejects zero Max", func(t *testing.T) {
		state := &counterState{}
		loop := LoopProcessor[counterState]{
			Max:   0,
			Until: func(_ *counterState) bool { return true },
			Body:  []Processor[counterState]{incProcessor{tag: "x"}},
		}
		if err := loop.Run(context.Background(), state); err == nil {
			t.Error("expected error for Max=0")
		}
	})

	t.Run("rejects nil Until", func(t *testing.T) {
		state := &counterState{}
		loop := LoopProcessor[counterState]{
			Max:  3,
			Body: []Processor[counterState]{incProcessor{tag: "x"}},
		}
		if err := loop.Run(context.Background(), state); err == nil {
			t.Error("expected error for nil Until")
		}
	})
}

type ProcessorFunc[T any] func(context.Context, *T) error

func (f ProcessorFunc[T]) Run(ctx context.Context, state *T) error { return f(ctx, state) }
