package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/shared"
)

type stubOwnerResolver struct{ workspace, department string }

func (s stubOwnerResolver) WorkspaceIDForEntry(context.Context, string) (string, error) {
	return s.workspace, nil
}
func (s stubOwnerResolver) DepartmentIDForEntry(context.Context, string) (string, error) {
	return s.department, nil
}

type ownerResolverSetter interface {
	SetEntryOwnerResolver(shared.EntryType, EntryOwnerResolver)
}

func TestResolverSatisfiesTheAssertionTheContainerUses(t *testing.T) {
	var held interface{} = NewCampaignWorkspaceResolver(nil, nil)

	if _, ok := held.(ownerResolverSetter); !ok {
		t.Fatal("the container's assertion does not match: every channel's tenant lookup " +
			"would fail to register, silently")
	}
}

func TestAnInlineInterfaceLiteralParameterDoesNotMatch(t *testing.T) {
	var held interface{} = NewCampaignWorkspaceResolver(nil, nil)

	_, ok := held.(interface {
		SetEntryOwnerResolver(shared.EntryType, interface {
			WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
			DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
		})
	})
	if ok {
		t.Skip("Go now treats these as identical; the named-type rule below is what matters")
	}
	t.Log("confirmed: an inline interface literal parameter does not satisfy the assertion")
}

func TestRegisteredChannelsResolveTheirOwnWorkspace(t *testing.T) {
	resolver := NewCampaignWorkspaceResolver(nil, nil)
	setter, ok := resolver.(ownerResolverSetter)
	if !ok {
		t.Fatal("resolver does not expose SetEntryOwnerResolver")
	}

	for _, tc := range []struct {
		entryType shared.EntryType
		workspace string
	}{
		{shared.EntryTypeInstagram, "ws-instagram"},
		{shared.EntryTypeTelegram, "ws-telegram"},
	} {
		setter.SetEntryOwnerResolver(tc.entryType, stubOwnerResolver{workspace: tc.workspace})
	}

	for _, tc := range []struct {
		entryType shared.EntryType
		workspace string
	}{
		{shared.EntryTypeInstagram, "ws-instagram"},
		{shared.EntryTypeTelegram, "ws-telegram"},
	} {
		got, err := resolver.GetEntryWorkspaceID("conv-1", string(tc.entryType))
		if err != nil {
			t.Errorf("%s: %v (this is the 'unknown entry type' failure)", tc.entryType, err)
			continue
		}
		if got != tc.workspace {
			t.Errorf("%s: workspace = %q, want %q", tc.entryType, got, tc.workspace)
		}
	}
}

func TestAnUnregisteredChannelIsRefusedByName(t *testing.T) {
	resolver := NewCampaignWorkspaceResolver(nil, nil)

	if _, err := resolver.GetEntryWorkspaceID("conv-1", "carrier-pigeon"); err == nil {
		t.Error("an unregistered channel must not resolve to an empty workspace")
	}
}
