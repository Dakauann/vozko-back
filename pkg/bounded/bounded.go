package bounded

import (
	"context"
	"sync"
)

func ForEach[T any](ctx context.Context, items []T, workers int, fn func(context.Context, T)) {
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(item T) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(ctx, item)
		}(item)
	}
	wg.Wait()
}
