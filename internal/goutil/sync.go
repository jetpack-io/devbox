package goutil

import (
	"context"
	"sync"
)

type once[T any] struct {
	once   sync.Once
	fn     func(context.Context) (T, error)
	result T
	err    error
}

func OnceWithContext[T any](fn func(context.Context) (T, error)) *once[T] {
	return &once[T]{fn: fn}
}

func (o *once[T]) Do(ctx context.Context) (T, error) {
	o.once.Do(func() {
		o.result, o.err = o.fn(ctx)
	})
	return o.result, o.err
}
