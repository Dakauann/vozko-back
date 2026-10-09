package lead

import (
	"testing"

	"vozko/domain/calls/calllist"
	calllist_usecase "vozko/usecases/calls/calllist"
	leadaction_usecase "vozko/usecases/leadaction"
)

func TestAStartedCallListAnswersTheVerdictTheCallListServiceDecided(t *testing.T) {
	if got := callListResponse(leadaction_usecase.Outcome{}); got != nil {
		t.Fatalf("an action without a call list answered %+v", got)
	}
	active := &calllist.List{ID: "list-1", Status: calllist.StatusActive, AssigneeIDs: []string{"worker-1"}}
	verdict := calllist.ListVerdict{AcceptsOutcomes: true, StatusMoves: []calllist.Status{calllist.StatusPaused}}
	got := callListResponse(leadaction_usecase.Outcome{CallList: &calllist_usecase.ListView{List: active, Verdict: verdict}})
	if got == nil || got.ID != "list-1" || !got.AcceptsOutcomes || len(got.StatusMoves) != 1 || got.StatusMoves[0] != "paused" {
		t.Fatalf("response = %+v, want the verdict as the service gave it", got)
	}
}
