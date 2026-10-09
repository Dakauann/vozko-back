package leadaction_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	"vozko/domain/selection"
	wd "vozko/domain/workspace/workspace_department"
	leadsend_usecase "vozko/usecases/leadsend"
)

type fakeSends struct {
	checkErr   error
	checks     int
	quotes     []int
	prepared   []leadsend_usecase.PrepareRequest
	reviews    map[string]*campaign.SendReview
	existings  []string
	prepareErr error
	partsLeft  bool
	during     func()
}

func (f *fakeSends) HasParts(_ context.Context, _ Actor, _ leadaction.Action, base string) (bool, error) {
	return f.partsLeft || f.reviews[base] != nil, nil
}

func (f *fakeSends) Check(context.Context, Actor, leadaction.Action, leadaction.SendParams) error {
	f.checks++
	return f.checkErr
}

func (f *fakeSends) Quote(_ context.Context, _ Actor, _ leadaction.Action, _ leadaction.SendParams, selected int) (*campaign.SendQuote, error) {
	f.quotes = append(f.quotes, selected)
	return &campaign.SendQuote{Count: selected, Parts: campaign.PartsNeeded(selected)}, nil
}

func (f *fakeSends) Existing(_ context.Context, _ Actor, _ *wd.DepartmentFilter, _ leadaction.Action, base string) (*campaign.SendReview, error) {
	f.existings = append(f.existings, base)
	return f.reviews[base], nil
}

func (f *fakeSends) Prepare(_ context.Context, req leadsend_usecase.PrepareRequest) (*campaign.SendReview, error) {
	f.prepared = append(f.prepared, req)
	if f.during != nil {
		during := f.during
		f.during = nil
		during()
	}
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	review := &campaign.SendReview{Channel: req.Action.Channel(), Parts: []campaign.SendPart{{CampaignID: "c-" + req.Base}}}
	if f.reviews == nil {
		f.reviews = map[string]*campaign.SendReview{}
	}
	f.reviews[req.Base] = review
	return review, nil
}

func templateSend(s selection.Selection, templateID string) Request {
	return Request{
		Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionSendTemplate, DepartmentFilter: &wd.DepartmentFilter{IsOwnerOrAdmin: true},
		Params:    leadaction.Params{Send: &leadaction.SendParams{Name: "Matrículas", BusinessPhoneID: "bp-1", TemplateID: templateID}},
		Selection: s, IdempotencyKey: "key-send",
	}
}

func withSendPermissions(h *harness) {
	for _, p := range []string{"whatsapp_campaigns:create", "whatsapp_campaigns:start", "whatsapp_templates:send", "whatsapp_templates:read", "business_phones:read", "conversations:read"} {
		h.perms[manager][p] = true
	}
}

func TestASendPreviewChecksFirstThenCountsOnceAndQuotesTheSelection(t *testing.T) {
	h := newHarness(12)
	withSendPermissions(h)
	h.sends.checkErr = campaign.ErrCreationScopeMissing
	if _, err := h.svc.Preview(context.Background(), templateSend(matchingAll(0), "tpl-1")); !errors.Is(err, campaign.ErrCreationScopeMissing) {
		t.Fatalf("Preview = %v, want the send check refusal", err)
	}
	if len(h.selections.counted) != 0 {
		t.Fatal("a refused send was counted")
	}
	h.sends.checkErr = nil
	p, err := h.svc.Preview(context.Background(), templateSend(matchingAll(0), "tpl-1"))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if p.Status != leadaction.PreviewDone || p.Send == nil || p.Send.Count != 12 || p.Result.Selected != 12 || len(h.selections.counted) != 1 {
		t.Fatalf("preview = %+v send = %+v", p, p.Send)
	}
}

func TestASendNeedsTheSendCapability(t *testing.T) {
	h := newHarness(3)
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(3), "tpl-1")); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("Start = %v, want ErrForbidden", err)
	}
	if h.sends.checks != 0 {
		t.Fatal("a forbidden send reached the send checks")
	}
}

func TestStartingASendFreezesTheSelectionUnderTheSendKeyAndPreparesTheStoppedCampaign(t *testing.T) {
	h := newHarness(5)
	withSendPermissions(h)
	out, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(h.sends.prepared) != 1 || out.Send == nil {
		t.Fatalf("prepared %d, outcome %+v", len(h.sends.prepared), out)
	}
	req := h.sends.prepared[0]
	if req.SnapshotID != req.Base || h.selections.freezes != 1 || req.Departments == nil || req.Params.TemplateID != "tpl-1" {
		t.Fatalf("prepare request = %+v", req)
	}
	again, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1"))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(h.sends.prepared) != 1 || h.selections.freezes != 1 || again.Send.Parts[0].CampaignID != out.Send.Parts[0].CampaignID {
		t.Fatalf("a retried send prepared again: %d prepares, %d freezes", len(h.sends.prepared), h.selections.freezes)
	}
}

func TestOneSendKeyCannotPrepareTwoDifferentSends(t *testing.T) {
	h := newHarness(5)
	withSendPermissions(h)
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-2")); !errors.Is(err, leadaction.ErrIdempotencyKeyReused) {
		t.Fatalf("Start with another template = %v, want ErrIdempotencyKeyReused", err)
	}
	if len(h.sends.prepared) != 1 {
		t.Fatal("the reused key prepared a second send")
	}
}

func TestARefusedSendFreesItsKeyForTheCorrectedRetry(t *testing.T) {
	h := newHarness(5)
	withSendPermissions(h)
	var changed *selection.CountChangedError
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(4), "tpl-1")); !errors.As(err, &changed) {
		t.Fatalf("Start = %v, want the selection changed", err)
	}
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1")); err != nil {
		t.Fatalf("the retry with the new count = %v, the dialog key must still be usable", err)
	}
	if len(h.sends.prepared) != 1 {
		t.Fatalf("prepared %d, want one", len(h.sends.prepared))
	}
}

func TestAFailureAfterAPartWasCreatedKeepsTheKeyReserved(t *testing.T) {
	h := newHarness(5)
	withSendPermissions(h)
	h.sends.prepareErr, h.sends.partsLeft = errors.New("entry insert timed out"), true
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1")); err == nil {
		t.Fatal("a failed preparation reported success")
	}
	h.sends.prepareErr = nil
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-2")); !errors.Is(err, leadaction.ErrIdempotencyKeyReused) {
		t.Fatalf("another request on a key that holds a part = %v, want ErrIdempotencyKeyReused", err)
	}
}

func TestASelectionTooLargeForOneCampaignIsRefusedBeforeItIsFrozen(t *testing.T) {
	h := newHarness(5)
	withSendPermissions(h)
	h.selections.matched, h.selections.selectedCount = campaign.MaxEntries+1, campaign.MaxEntries+1
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(campaign.MaxEntries+1), "tpl-1")); !errors.Is(err, campaign.ErrSelectionOverCampaignCap) {
		t.Fatalf("Start = %v, want ErrSelectionOverCampaignCap", err)
	}
	if h.selections.freezes != 0 || len(h.sends.prepared) != 0 {
		t.Fatalf("freezes %d prepares %d, nothing may be written for a refused size", h.selections.freezes, len(h.sends.prepared))
	}
}

func TestExclusionsThatBringTheSelectionUnderTheCapAreCounted(t *testing.T) {
	h := newHarness(5)
	withSendPermissions(h)
	h.selections.matched = campaign.MaxEntries + 1
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(campaign.MaxEntries+1), "tpl-1")); err != nil {
		t.Fatalf("Start = %v, the selected count fits one campaign", err)
	}
}

func TestASecondRequestWhileTheSendIsBeingPreparedWaits(t *testing.T) {
	h := newHarness(5)
	withSendPermissions(h)
	var concurrent error
	h.sends.during = func() {
		_, concurrent = h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1"))
	}
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !errors.Is(concurrent, campaign.ErrSendPreparing) {
		t.Fatalf("the concurrent request = %v, want ErrSendPreparing", concurrent)
	}
	if len(h.sends.prepared) != 1 {
		t.Fatalf("prepared %d, want one", len(h.sends.prepared))
	}
	if _, err := h.svc.Start(context.Background(), templateSend(matchingAll(5), "tpl-1")); err != nil {
		t.Fatalf("a retry after the preparation = %v, the lease must be released", err)
	}
}

func TestAnEmptySelectionPreparesNothing(t *testing.T) {
	h := newHarness(0)
	withSendPermissions(h)
	if _, err := h.svc.Start(context.Background(), templateSend(selection.Selection{Mode: selection.ModeIDs, IDs: []string{"00000000-0000-4000-8000-000000000001"}}, "tpl-1")); !errors.Is(err, leadaction.ErrSelectionEmpty) {
		t.Fatalf("Start = %v, want ErrSelectionEmpty", err)
	}
	if len(h.sends.prepared) != 0 {
		t.Fatal("an empty selection was prepared")
	}
}
