package goutil

import (
	"context"
	"sync"
)

type onceValue[T any] struct {
	once   sync.Once
	fn     func(context.Context) T
	result T
}

func OnceValueWithContext[T any](fn func(context.Context) T) *onceValue[T] {
	return &onceValue[T]{fn: fn}
}

func (o *onceValue[T]) Do(ctx context.Context) T {
	o.once.Do(func() {
		o.result = o.fn(ctx)
	})
	return o.result
}

type onceValues[T any] struct {
	once   sync.Once
	fn     func(context.Context) (T, error)
	result T
	err    error
}

func OnceValuesWithContext[T any](fn func(context.Context) (T, error)) *onceValues[T] {
	return &onceValues[T]{fn: fn}
}

func (o *onceValues[T]) Do(ctx context.Context) (T, error) {
	o.once.Do(func() {
		o.result, o.err = o.fn(ctx)
	})
	return o.result, o.err
}
