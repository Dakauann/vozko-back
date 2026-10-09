package lead_usecase

import (
	"context"

	"vozko/domain/cache"
	"vozko/domain/lead"
)

func memoSection[T any](
	ctx context.Context,
	caching SectionCaching,
	prefix string,
	workspaceID string,
	key lead.SectionKey,
	read func(context.Context) (T, error),
) (T, error) {
	compute := func(ctx context.Context) (T, error) {
		var out T
		err := cache.Gated(ctx, caching.Gate, func(ctx context.Context) error {
			var err error
			out, err = read(ctx)
			return err
		})
		return out, err
	}
	generation, err := caching.Versions.Version(workspaceID)
	if err != nil {
		return compute(ctx)
	}
	fingerprint, err := key.Fingerprint()
	if err != nil {
		return compute(ctx)
	}
	return cache.Remember(ctx, caching.Memo, prefix+":"+workspaceID+":"+generation+":"+fingerprint, caching.TTL, compute)
}
