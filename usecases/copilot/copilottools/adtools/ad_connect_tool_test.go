package adtools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func TestConnectingAnAdAccountOnlyShowsTheButton(t *testing.T) {
	tool := NewConnectAdAccountTool()
	if meta := tool.Meta(); meta.Mutating || meta.Resource != workspace.ResourceAds || meta.Action != workspace.ActionCreate {
		t.Fatalf("meta %+v", meta)
	}
	result := tool.Execute(context.Background(), tooltest.AdContext, nil)
	if result.Status != copilot.StatusOK || result.Card == nil || result.Card.Kind != copilot.ActionConnectAdAccount {
		t.Fatalf("result %+v", result)
	}
}
