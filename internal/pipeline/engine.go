package pipeline

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrIssueUnclear = errors.New("issue requirements unclear")
	ErrNoChanges    = errors.New("no changes produced")
	ErrLoopMax      = errors.New("loop reached max iterations")
)

type Processor[T any] interface {
	Run(ctx context.Context, state *T) error
}

func Execute[T any](ctx context.Context, state *T, steps []Processor[T]) error {
	for _, p := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.Run(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

type LoopProcessor[T any] struct {
	Body  []Processor[T]
	Until func(*T) bool
	Max   int
}

func (l LoopProcessor[T]) Run(ctx context.Context, state *T) error {
	if l.Max <= 0 {
		return fmt.Errorf("LoopProcessor: Max must be > 0, got %d", l.Max)
	}
	if l.Until == nil {
		return errors.New("LoopProcessor: Until must not be nil")
	}
	for i := 0; i < l.Max; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := Execute(ctx, state, l.Body); err != nil {
			return err
		}
		if l.Until(state) {
			return nil
		}
	}
	return ErrLoopMax
}
