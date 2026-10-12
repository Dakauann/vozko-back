package openroutergen

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"
)

var unsentRetryDelays = []time.Duration{time.Second, 3 * time.Second}

func send(ctx context.Context, client *http.Client, delays []time.Duration, build func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err == nil || !neverSent(err) || attempt >= len(delays) || ctx.Err() != nil {
			return resp, err
		}
		log.Printf("[media-generation] the request never reached the provider (%v); sending it again in %s", err, delays[attempt])
		if !waitFor(ctx, delays[attempt]) {
			return nil, err
		}
	}
}

func neverSent(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func waitFor(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
