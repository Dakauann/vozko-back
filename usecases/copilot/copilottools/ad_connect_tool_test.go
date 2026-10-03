package copilottools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/workspace"
)

func TestConnectingAnAdAccountOnlyShowsTheButton(t *testing.T) {
	tool := NewConnectAdAccountTool()
	if meta := tool.Meta(); meta.Mutating || meta.Resource != workspace.ResourceAds || meta.Action != workspace.ActionCreate {
		t.Fatalf("meta %+v", meta)
	}
	result := tool.Execute(context.Background(), adContext, nil)
	if result.Status != copilot.StatusOK || result.Card == nil || result.Card.Kind != copilot.ActionConnectAdAccount {
		t.Fatalf("result %+v", result)
	}
}
