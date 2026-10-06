package copilottools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

func pausedChildren() []*advertising.Object {
	return []*advertising.Object{
		{MetaID: "120201", Name: "Conjunto SP", Level: advertising.LevelAdSet},
		{MetaID: "120301", Name: "Anúncio SP", Level: advertising.LevelAd},
	}
}

func TestTurningOnACampaignSaysWhatStaysOff(t *testing.T) {
	deps, manager, _ := adDeps()
	manager.offBelow = pausedChildren()
	tool := NewTurnOnAdTool(deps)
	args := map[string]interface{}{"meta_id": "120200"}
	fields := fieldsOf(tool.(copilot.Describer).Describe(context.Background(), adContext, args))
	if !strings.HasPrefix(fields["below"], "continuam desligados: ") || !strings.Contains(fields["below"], "Conjunto SP") {
		t.Fatalf("fields %+v", fields)
	}
	result := tool.Execute(context.Background(), adContext, args)
	off := result.Data.(map[string]interface{})["still_off"].([]map[string]string)
	if result.Status != copilot.StatusOK || len(off) != 2 || off[0]["level"] != "adset" || *manager.withBelow {
		t.Fatalf("result %+v", result)
	}
}

func TestTurningOnACampaignWithEverythingBelowAsksForItInTheSameApproval(t *testing.T) {
	deps, manager, _ := adDeps()
	manager.offBelow = pausedChildren()
	tool := NewTurnOnAdTool(deps)
	args := map[string]interface{}{"meta_id": "120200", "include_below": true}
	fields := fieldsOf(tool.(copilot.Describer).Describe(context.Background(), adContext, args))
	if !strings.HasPrefix(fields["below"], "também liga: ") {
		t.Fatalf("fields %+v", fields)
	}
	result := tool.Execute(context.Background(), adContext, args)
	if off := result.Data.(map[string]interface{})["still_off"].([]map[string]string); len(off) != 0 || !*manager.withBelow {
		t.Fatalf("result %+v", result)
	}
}
