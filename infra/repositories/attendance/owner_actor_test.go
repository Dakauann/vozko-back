package attendance_repository

import (
	"strings"
	"testing"

	"vozko/infra/database/schema"
)

func TestMetricsProjectTheOwnerAsAnActorID(t *testing.T) {
	// inbox_assignments stores an agent or workflow as its bare uuid plus
	// assignee_kind. Metrics must see ai:<id> / workflow:<id>, or an agent
	// would be grouped and labelled like a person with a raw uuid for a name.
	for _, src := range channelSources {
		p := src.projection("TRUE")
		if !strings.Contains(p, ownerActorIDSQL+" AS assigned_user_id") {
			t.Errorf("%s: projection does not use the owner actor id", src.EntryType)
		}
	}
	for _, want := range []string{"'ai:'", "'workflow:'", "ia.assignee_kind"} {
		if !strings.Contains(ownerActorIDSQL, want) {
			t.Errorf("owner actor id SQL is missing %s", want)
		}
	}
	aliasModels["ia"] = &schema.InboxAssignment{}
	assertColumnsExist(t, "owner actor id", ownerActorIDSQL)
}

func TestOwnerBreakdownsNameAgentsAndWorkflows(t *testing.T) {
	for _, want := range []string{"LEFT JOIN agents", "LEFT JOIN workflows", "'ai:' || ", "'workflow:' || "} {
		if !strings.Contains(ownerLabelJoinsSQL("m"), want) {
			t.Errorf("owner label joins are missing %q", want)
		}
	}
	if !strings.Contains(ownerLabelSQL("m"), "m.assigned_user_id") {
		t.Error("the label must fall back to the id only as a last resort")
	}
}
