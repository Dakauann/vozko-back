package mercadolivre

import (
	"context"
	"testing"
)

func TestBackgroundPriorityTravelsInTheContext(t *testing.T) {
	if IsBackground(context.Background()) {
		t.Fatal("a plain context is live traffic")
	}
	ctx := WithBackgroundPriority(context.Background())
	if !IsBackground(ctx) {
		t.Fatal("the marked context must read as background")
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	if !IsBackground(child) {
		t.Fatal("derived contexts keep the priority")
	}
}
