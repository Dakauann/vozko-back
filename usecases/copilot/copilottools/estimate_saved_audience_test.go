package copilottools

import (
	"context"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

type recordingTargeting struct {
	accountID string
	targeting advertising.Targeting
	goal      advertising.OptimizationGoal
}

func (r *recordingTargeting) Targeting(context.Context, string, string, advertising.TargetingSearchKind, string) ([]advertising.TargetingOption, error) {
	return nil, nil
}

func (r *recordingTargeting) Reach(_ context.Context, _, accountID string, t advertising.Targeting, _ advertising.Placements, goal advertising.OptimizationGoal) (*advertising.ReachEstimate, error) {
	r.accountID, r.targeting, r.goal = accountID, t, goal
	return &advertising.ReachEstimate{Lower: 41000000, Upper: 48000000, Ready: true}, nil
}

func savedAudienceTool() (copilot.Tool, *recordingTargeting) {
	f := newManageFixture()
	detail := adSetDetail("120201", "Cadastros")
	detail.Targeting.ExcludedLocations = []advertising.GeoLocation{{Kind: advertising.LocationRegion, Key: "460", Name: "São Paulo"}}
	f.editor.details["120201"] = detail
	f.editor.details["120301"].Object.AdSetMetaID = "120201"
	targeting := &recordingTargeting{}
	deps := AdsDeps{Accounts: manageAccounts{account: f.account}, Editor: f.editor, Targeting: targeting}
	return NewEstimateAdAudienceTool(deps), targeting
}

func TestEstimateUsesTheSavedAudienceOfAnAdSetOrItsAd(t *testing.T) {
	for _, id := range []string{"120201", "120301"} {
		tool, targeting := savedAudienceTool()
		result := tool.Execute(context.Background(), adContext, map[string]interface{}{"meta_id": id})
		data, _ := result.Data.(map[string]interface{})
		if result.Status != copilot.StatusOK || data["people_min"] != int64(41000000) {
			t.Fatalf("%s: result %+v", id, result)
		}
		if len(targeting.targeting.ExcludedLocations) != 1 || targeting.goal != advertising.GoalConversations || targeting.accountID != adAccountUUID {
			t.Fatalf("%s: estimated %+v", id, targeting)
		}
	}
}

func TestEstimateWithoutAnAdSetNeedsTheAudienceFields(t *testing.T) {
	tool, targeting := savedAudienceTool()
	if result := tool.Execute(context.Background(), adContext, map[string]interface{}{"objective": "OUTCOME_LEADS"}); result.Status != copilot.StatusError {
		t.Fatalf("result %+v", result)
	}
	if result := tool.Execute(context.Background(), adContext, map[string]interface{}{"meta_id": "120200"}); result.Status == copilot.StatusOK {
		t.Fatalf("a campaign has no audience of its own: %+v", result)
	}
	if targeting.accountID != "" {
		t.Fatal("estimated without a valid audience")
	}
}
