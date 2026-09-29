package livedecisions_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
)

func quietTarget() Trigger {
	return Trigger{WorkspaceID: "ws", EntryID: "entry", EntryType: shared.EntryTypeWhatsApp}
}

func TestTheStageGoesToTheLLMUnlessALiveReadSettledIt(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	if !h.service.StageNeedsReview(ctx, quietTarget()) {
		t.Fatal("no live read yet: the LLM decides the stage")
	}
	_, _ = h.reads.Save(ctx, ld.LiveRead{WorkspaceID: "ws", EntryID: "entry", EntryType: "whatsapp", StageSettled: true, DecidedThrough: t0})
	if h.service.StageNeedsReview(ctx, quietTarget()) {
		t.Fatal("a confident live stage decision leaves nothing for the LLM")
	}
	_, _ = h.reads.Save(ctx, ld.LiveRead{WorkspaceID: "ws", EntryID: "entry", EntryType: "whatsapp", DecidedThrough: t0.Add(time.Second)})
	if !h.service.StageNeedsReview(ctx, quietTarget()) {
		t.Fatal("a live read that did not settle the stage leaves it to the LLM")
	}
	_, _ = h.reads.Save(ctx, ld.LiveRead{WorkspaceID: "ws", EntryID: "entry", EntryType: "whatsapp", Qualification: "hot_lead", DecidedThrough: t0.Add(2 * time.Second)})
	if !h.service.StageNeedsReview(ctx, quietTarget()) {
		t.Fatal("labels alone never settle the stage: a conversation with no stage question still goes to the LLM")
	}
}

func TestTheQuietGateSkipsTheLLMOnlyOnAConfidentNothing(t *testing.T) {
	h := newHarness()
	h.model.result = decision.Result{Answers: map[string]decision.Answer{
		ld.QuestionNewMemory:  {Kind: decision.KindYesNo, Yes: 0.05},
		ld.QuestionDealChange: {Kind: decision.KindChoice, Choice: ld.DealChangeNone, Confidence: 0.93},
	}, CostMicros: 30}
	gate := h.service.QuietGate(context.Background(), quietTarget(), true, true, "memória: nenhuma", "sem oportunidades")
	if gate.NeedsMemory || gate.NeedsDeals {
		t.Fatalf("gate = %+v", gate)
	}
	request := h.model.requests[0]
	state := request.State.(map[string]any)
	if request.Purpose != ld.PurposeQuietGate || state["memoria"] != "memória: nenhuma" || state["oportunidades"] != "sem oportunidades" {
		t.Fatalf("request = %+v", request)
	}
	record := h.log.last()
	if record.Purpose != ld.PurposeQuietGate || len(record.Effects) != 2 {
		t.Fatalf("record = %+v", record)
	}
}

func TestTheQuietGateFallsBackToTheLLMOnAnyDoubt(t *testing.T) {
	h := newHarness()
	h.model.err = decision.ErrUnavailable
	gate := h.service.QuietGate(context.Background(), quietTarget(), true, true, "", "")
	if !gate.NeedsMemory || !gate.NeedsDeals {
		t.Fatalf("a failed gate must leave the LLM in charge: %+v", gate)
	}
	broke := newHarness()
	broke.service.deps.Funds = allow(false)
	if gate := broke.service.QuietGate(context.Background(), quietTarget(), true, true, "", ""); !gate.NeedsMemory || !gate.NeedsDeals {
		t.Fatalf("without balance the gate cannot run: %+v", gate)
	}
	if gate := h.service.QuietGate(context.Background(), quietTarget(), false, false, "", ""); gate != (ld.QuietGate{}) {
		t.Fatalf("nothing wanted: %+v", gate)
	}
}
