package tools

import (
	"context"
	"testing"
)

func TestCallIDTravelsWithTheContext(t *testing.T) {
	if got := CallIDFrom(context.Background()); got != "" {
		t.Fatalf("empty context = %q", got)
	}
	ctx := WithCallID(context.Background(), " call_9 ")
	if got := CallIDFrom(ctx); got != "call_9" {
		t.Fatalf("call id = %q", got)
	}
	if got := CallIDFrom(WithCallID(ctx, "")); got != "call_9" {
		t.Fatalf("an empty id must not erase the known one: %q", got)
	}
}
