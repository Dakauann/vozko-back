package workspace

import (
	"errors"
	"reflect"
	"testing"
)

func TestParsePermissionEntriesAcceptsOnlyTheCatalog(t *testing.T) {
	got, err := ParsePermissionEntries([]string{" whatsapp_campaigns:read ", "whatsapp_campaigns:start"})
	want := []PermissionEntry{{Resource: ResourceWhatsAppCampaigns, Action: ActionRead}, {Resource: ResourceWhatsAppCampaigns, Action: ActionStart}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v err %v", got, err)
	}
	for _, raw := range []string{"whatsapp_campaigns", "invented:read", "whatsapp_campaigns:fly"} {
		if _, err := ParsePermissionEntries([]string{raw}); !errors.Is(err, ErrInvalidResource) && !errors.Is(err, ErrInvalidAction) {
			t.Errorf("%q accepted: %v", raw, err)
		}
	}
}

func TestApplyPermissionChangesGrantsRevokesAndStaysSorted(t *testing.T) {
	current := []PermissionEntry{{Resource: ResourceLabels, Action: ActionRead}, {Resource: ResourceAgents, Action: ActionRead}}
	got := ApplyPermissionChanges(current,
		[]PermissionEntry{{Resource: ResourceAgents, Action: ActionRead}, {Resource: ResourceAgents, Action: ActionCreate}},
		[]PermissionEntry{{Resource: ResourceLabels, Action: ActionRead}})
	want := []PermissionEntry{{Resource: ResourceAgents, Action: ActionCreate}, {Resource: ResourceAgents, Action: ActionRead}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestUnmetRequirementsNamesWhatAGrantNeeds(t *testing.T) {
	details := PermissionEntry{Resource: ResourceAgents, Action: ActionReadDetails}
	unmet := UnmetRequirements([]PermissionEntry{details})
	if len(unmet[details]) == 0 {
		t.Fatalf("read_details without its prerequisites passed: %+v", unmet)
	}
	complete := append([]PermissionEntry{details}, unmet[details]...)
	if left := UnmetRequirements(complete); len(left) != 0 {
		t.Fatalf("still unmet: %+v", left)
	}
}

func TestDescribePermissionUsesTheCatalogText(t *testing.T) {
	if got := DescribePermission(PermissionEntry{Resource: ResourceWhatsAppCampaigns, Action: ActionStart}); got != "Iniciar envio de campanhas WhatsApp" {
		t.Fatalf("description = %q", got)
	}
}
