package workspace

import "testing"

func TestRetiredResourcesAreDroppedNotRejected(t *testing.T) {
	perms := []PermissionEntry{
		{Resource: ResourceConversations, Action: ActionRead},
		{Resource: Resource("sip_trunks"), Action: ActionRead},
		{Resource: ResourceLeads, Action: ActionCreate},
		{Resource: Resource("usage"), Action: ActionRead},
		{Resource: Resource("affiliate"), Action: ActionRead},
		{Resource: ResourceBalance, Action: ActionRead},
	}

	kept, dropped := DropRetiredResources(perms)

	if len(kept) != 3 {
		t.Errorf("kept %d entries, want the 3 live ones: %v", len(kept), kept)
	}
	for _, p := range kept {
		if !p.Resource.IsValid() {
			t.Errorf("kept a resource this build does not define: %q", p.Resource)
		}
	}
	if len(dropped) != 3 {
		t.Errorf("reported %d dropped, want 3: %v", len(dropped), dropped)
	}

	want := []Resource{ResourceConversations, ResourceLeads, ResourceBalance}
	for i, w := range want {
		if kept[i].Resource != w {
			t.Errorf("kept[%d] = %q, want %q — order must be preserved", i, kept[i].Resource, w)
		}
	}
}

func TestDroppedResourcesAreReportedOnce(t *testing.T) {
	perms := []PermissionEntry{
		{Resource: Resource("sip_trunks"), Action: ActionRead},
		{Resource: Resource("sip_trunks"), Action: ActionCreate},
		{Resource: Resource("sip_trunks"), Action: ActionDelete},
	}

	kept, dropped := DropRetiredResources(perms)

	if len(kept) != 0 {
		t.Errorf("kept %v, want nothing", kept)
	}
	if len(dropped) != 1 || dropped[0] != Resource("sip_trunks") {
		t.Errorf("dropped = %v, want [sip_trunks] once", dropped)
	}
}

func TestALiveSetIsUntouched(t *testing.T) {
	perms := []PermissionEntry{
		{Resource: ResourceConversations, Action: ActionRead},
		{Resource: ResourceLeads, Action: ActionCreate},
	}

	kept, dropped := DropRetiredResources(perms)

	if len(dropped) != 0 {
		t.Errorf("dropped %v from a fully live set", dropped)
	}
	if len(kept) != len(perms) {
		t.Errorf("kept %d of %d", len(kept), len(perms))
	}
}

func TestAKnownResourceStillValidatesItsAction(t *testing.T) {
	perms := []PermissionEntry{{Resource: ResourceConversations, Action: Action("not_an_action")}}

	kept, dropped := DropRetiredResources(perms)

	if len(dropped) != 0 {
		t.Fatalf("dropped %v — a known resource must survive this pass", dropped)
	}
	if len(kept) != 1 {
		t.Fatalf("kept %d, want the entry to reach the action check", len(kept))
	}
	if kept[0].Action.IsValid() {
		t.Error("the bogus action must still be caught by the caller's own check")
	}
}
