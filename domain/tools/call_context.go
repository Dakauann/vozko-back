package tools

import (
	"context"
	"strings"
)

type callIDKey struct{}

func WithCallID(ctx context.Context, callID string) context.Context {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return ctx
	}
	return context.WithValue(ctx, callIDKey{}, callID)
}

func CallIDFrom(ctx context.Context) string {
	callID, _ := ctx.Value(callIDKey{}).(string)
	return callID
}
