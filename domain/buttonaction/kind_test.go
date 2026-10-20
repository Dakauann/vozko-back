package buttonaction

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

func TestIsExitOnlyForOptOutAndConsentGrant(t *testing.T) {
	cases := map[Kind]bool{
		KindOptOut:        true,
		KindConsentGrant:  true,
		KindHandOff:       false,
		KindDelegate:      false,
		KindStartWorkflow: false,
		KindCallList:      false,
		KindClassify:      false,
		KindCreateDeal:    false,
		Kind("nope"):      false,
	}
	for kind, want := range cases {
		if got := IsExit(kind); got != want {
			t.Errorf("IsExit(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestKnownKinds(t *testing.T) {
	for _, kind := range Kinds() {
		if !kind.Known() {
			t.Errorf("%q should be known", kind)
		}
	}
	for _, kind := range []Kind{"", "OPT_OUT", "send_template"} {
		if kind.Known() {
			t.Errorf("%q should not be known", kind)
		}
	}
}

func TestEachKindNamesTheCapabilityItNeeds(t *testing.T) {
	cases := []struct {
		kind         Kind
		capabilities []workspace.CapabilityKey
		permissions  []workspace.PermissionEntry
	}{
		{kind: KindOptOut},
		{kind: KindConsentGrant, capabilities: []workspace.CapabilityKey{CapabilityConsent}},
		{kind: KindHandOff, capabilities: []workspace.CapabilityKey{"leads.assign"}},
		{kind: KindDelegate, capabilities: []workspace.CapabilityKey{"inbox.delegate"}},
		{kind: KindCallList, capabilities: []workspace.CapabilityKey{"call_lists.manage"}},
		{kind: KindClassify, permissions: []workspace.PermissionEntry{{Resource: workspace.ResourceLeads, Action: workspace.ActionUpdate}}},
		{kind: KindStartWorkflow, capabilities: []workspace.CapabilityKey{"workflows.edit"}},
		{kind: KindCreateDeal, capabilities: []workspace.CapabilityKey{"deals.edit"}},
	}
	for _, tc := range cases {
		got, err := RequirementsOf(tc.kind)
		if err != nil {
			t.Fatalf("%s: %v", tc.kind, err)
		}
		if !sameKeys(got.Capabilities, tc.capabilities) || !sameEntries(got.Permissions, tc.permissions) {
			t.Errorf("%s: got %+v", tc.kind, got)
		}
	}
	if _, err := RequirementsOf("nope"); !errors.Is(err, ErrKindUnknown) {
		t.Errorf("unknown kind: %v, want ErrKindUnknown", err)
	}
}

func sameKeys(a, b []workspace.CapabilityKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameEntries(a, b []workspace.PermissionEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
