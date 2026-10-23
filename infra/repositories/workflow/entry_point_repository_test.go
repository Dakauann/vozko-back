package workflow_repository

import (
	"strings"
	"testing"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/workflow"
)

func TestEveryEntryPointQueryBindsExactlyTheArgumentsItIsGiven(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		args int
	}{
		{"published triggers", publishedTriggersSQL, 1},
		{"webhooks", webhooksSQL, 1},
		{"last deliveries", lastDeliveriesSQL, 1},
		{"last starts", lastStartsSQL, 1},
		{"forms", formsSQL, 2},
		{"agents", boundAgentsSQL, 3},
		{"template exits", templateExitsSQL, 2},
		{"campaign exits", campaignExitsSQL, 2},
		{"linked channels", linkedChannelsSQL, len(linkedChannelArgs("ws", []string{"wf"}))},
		{"enabled channels", enabledChannelsSQL, len(enabledChannelArgs("ws"))},
	} {
		if got := strings.Count(tc.sql, "?"); got != tc.args {
			t.Errorf("%s: %d placeholders, %d arguments", tc.name, got, tc.args)
		}
	}
}

func TestEveryEntryKindFilterBindsExactlyTheArgumentsItReturns(t *testing.T) {
	for _, kind := range workflow.EntryPointKinds() {
		filter, args := entryKindFilter("ws", kind)
		if got := strings.Count(filter, "?"); got != len(args) {
			t.Errorf("%s: %d placeholders, %d arguments", kind, got, len(args))
		}
		if filter == "FALSE" {
			t.Errorf("%s: a kind the read model produces must never filter everything out", kind)
		}
	}
	if filter, args := entryKindFilter("ws", workflow.EntryPointKind("unknown")); filter != "FALSE" || len(args) != 0 {
		t.Fatalf("an unknown kind must match nothing, got %s", filter)
	}
}

func TestEveryChannelTableIsAskedForItsLiveRowsInTheWorkspace(t *testing.T) {
	for _, c := range channelTables {
		if !strings.Contains(linkedChannelsSQL, "FROM "+c.table+" WHERE workspace_id = ? AND deleted_at IS NULL") ||
			!strings.Contains(enabledChannelsSQL, "FROM "+c.table+" WHERE workspace_id = ? AND deleted_at IS NULL") {
			t.Errorf("%s is not read by workspace and liveness", c.table)
		}
	}
}

func TestTheMercadoLivreAccountIsNamedBySellerNameFirst(t *testing.T) {
	for _, c := range channelTables {
		if c.kind == workflow.ChannelMercadoLivreAccount && c.name != mldomain.AccountNameSQL("") {
			t.Fatalf("mercado livre account name = %q", c.name)
		}
	}
}

func TestOnlyValidIDsReachAUUIDColumn(t *testing.T) {
	got := uuidsOf([]string{"not-a-uuid", " 4f1c2b6e-8e8a-4c1d-9f6e-2b1f8f9a0c11 ", ""})
	if len(got) != 1 || got[0] != "4f1c2b6e-8e8a-4c1d-9f6e-2b1f8f9a0c11" {
		t.Fatalf("uuids = %v", got)
	}
}
