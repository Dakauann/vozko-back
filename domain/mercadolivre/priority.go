package mercadolivre

import "context"

type backgroundPriorityKey struct{}

func WithBackgroundPriority(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundPriorityKey{}, true)
}

func IsBackground(ctx context.Context) bool {
	marked, _ := ctx.Value(backgroundPriorityKey{}).(bool)
	return marked
}
