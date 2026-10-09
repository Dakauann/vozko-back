package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/domain/workspace/workspace_plan"
)

type startAccess map[string]*wc.Campaign

func (a startAccess) Owned(workspaceID string, _ *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	c, ok := a[id]
	if !ok || c.WorkspaceID != workspaceID {
		return nil, wc.ErrCampaignNotFound
	}
	return c, nil
}

type startSubscription struct{ err error }

func (s startSubscription) Execute(string) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, s.err
}

type startDispatch struct{ started []string }

func (d *startDispatch) Dispatch(in wc.DispatchCampaignInput) error {
	d.started = append(d.started, in.CampaignID)
	return nil
}

func startFixture(subErr error) (wc.StartCampaignUseCase, *startDispatch) {
	dispatch := &startDispatch{}
	return NewStartCampaignUseCase(StartCampaignDeps{
		Access: startAccess{
			"ready": {ID: "ready", WorkspaceID: "ws1", Metrics: &wc.CampaignMetrics{TotalNumbers: 3, Pending: 3}},
			"empty": {ID: "empty", WorkspaceID: "ws1", Metrics: &wc.CampaignMetrics{}},
			"done":  {ID: "done", WorkspaceID: "ws1", Metrics: &wc.CampaignMetrics{TotalNumbers: 2, Processed: 2}},
		},
		Subscription: startSubscription{err: subErr},
		Dispatch:     dispatch,
	}), dispatch
}

func TestStartCampaignDispatchesAReadyCampaign(t *testing.T) {
	uc, dispatch := startFixture(nil)
	if _, err := uc.Start("ws1", nil, "ready"); err != nil || len(dispatch.started) != 1 {
		t.Fatalf("err %v started %v", err, dispatch.started)
	}
}

func TestStartCampaignRefusesWhatCannotBeSent(t *testing.T) {
	cases := map[string]struct {
		workspace, id string
		subErr        error
		want          error
	}{
		"foreign workspace": {"ws2", "ready", nil, wc.ErrCampaignNotFound},
		"no subscription":   {"ws1", "ready", errors.New("expired"), wc.ErrCampaignNoSubscription},
		"no numbers":        {"ws1", "empty", nil, wc.ErrCampaignNoNumbers},
		"all processed":     {"ws1", "done", nil, wc.ErrCampaignAllProcessed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			uc, dispatch := startFixture(c.subErr)
			if _, err := uc.Start(c.workspace, nil, c.id); !errors.Is(err, c.want) || len(dispatch.started) != 0 {
				t.Fatalf("err %v started %v", err, dispatch.started)
			}
		})
	}
}

type reviewedDispatch struct{ inputs []wc.DispatchCampaignInput }

func (d *reviewedDispatch) Dispatch(in wc.DispatchCampaignInput) error {
	d.inputs = append(d.inputs, in)
	return nil
}

func TestOnlyTheReviewedStartTellsTheDispatchTheSendWasReviewed(t *testing.T) {
	dispatch := &reviewedDispatch{}
	uc := NewStartCampaignUseCase(StartCampaignDeps{
		Access:       startAccess{"ready": {ID: "ready", WorkspaceID: "ws1", Metrics: &wc.CampaignMetrics{TotalNumbers: 3, Pending: 3}}},
		Subscription: startSubscription{},
		Dispatch:     dispatch,
	})
	reviewed, ok := uc.(wc.ReviewedStartUseCase)
	if !ok {
		t.Fatalf("the start use case cannot start a reviewed send")
	}
	if _, err := uc.Start("ws1", nil, "ready"); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if _, err := reviewed.StartReviewed("ws1", nil, "ready"); err != nil {
		t.Fatalf("StartReviewed = %v", err)
	}
	if _, err := reviewed.StartReviewed("ws2", nil, "ready"); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("StartReviewed of a foreign campaign = %v", err)
	}
	if len(dispatch.inputs) != 2 || dispatch.inputs[0].Reviewed || !dispatch.inputs[1].Reviewed {
		t.Fatalf("dispatched %+v", dispatch.inputs)
	}
}
