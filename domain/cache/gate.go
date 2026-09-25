package cache

import "context"

func Gated(ctx context.Context, gate Gate, fn func(context.Context) error) error {
	if gate == nil {
		return fn(ctx)
	}
	release, err := gate.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return fn(ctx)
}
