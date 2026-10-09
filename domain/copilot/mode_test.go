package copilot

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/tools"
)

func TestModesAreAClosedSetAndMissingMeansAsk(t *testing.T) {
	for raw, want := range map[string]Mode{"": ModeAsk, "ask": ModeAsk, "edit": ModeEdit, "full": ModeFull} {
		got, err := ParseMode(raw)
		if err != nil || got != want {
			t.Fatalf("ParseMode(%q) = %q, %v", raw, got, err)
		}
	}
	if _, err := ParseMode("yolo"); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("an unknown mode must be refused, got %v", err)
	}
}

type changeTool struct{ plainTool }

func (changeTool) Meta() Meta { return Meta{Mutating: true} }

type editTool struct{ plainTool }

func (editTool) NeedsApproval(mode Mode) bool { return mode == ModeAsk }

func (editTool) Definition() tools.Definition { return tools.Definition{Name: "edit"} }

func (editTool) Execute(context.Context, Context, map[string]interface{}) Result { return Result{} }

func TestTheModeOnlyGradesToolsThatAskForIt(t *testing.T) {
	for _, mode := range []Mode{ModeAsk, ModeEdit, ModeFull} {
		if !NeedsApproval(changeTool{}, mode) {
			t.Fatalf("a change outside the graded tools always asks, even in %s", mode)
		}
		if NeedsApproval(plainTool{}, mode) {
			t.Fatalf("a read never asks, in %s", mode)
		}
	}
	if !NeedsApproval(editTool{}, ModeAsk) || NeedsApproval(editTool{}, ModeEdit) || NeedsApproval(editTool{}, ModeFull) {
		t.Fatal("a graded tool follows its own rule")
	}
}
