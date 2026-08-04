package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/shared"
)

// The container registers each channel's tenant lookup through a type
// assertion, because the resolver is held as a domain interface. Instagram's
// assertion spelled the parameter as an inline `interface{ ... }` literal with
// the right methods, and Telegram's spelled it as the named EntryOwnerResolver.
//
// A defined type is never identical to a type literal, so only Telegram's
// matched. Instagram's was false on every boot, the registration never ran, and
// the compiler had nothing to complain about because a failed assertion is a
// runtime value, not an error. Every inbound Instagram message then failed to
// resolve a workspace: no inbox assignment, no initial tag, no agent reply.
//
// These pin the shape the container must assert against.

type stubOwnerResolver struct{ workspace, department string }

func (s stubOwnerResolver) WorkspaceIDForEntry(context.Context, string) (string, error) {
	return s.workspace, nil
}
func (s stubOwnerResolver) DepartmentIDForEntry(context.Context, string) (string, error) {
	return s.department, nil
}

// The assertion the container performs, written exactly as production writes it.
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

// The shape that shipped. An inline literal parameter never matches a method
// declared with the named type, whatever its method set.
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
	// Documented so nobody 'simplifies' the container back to the literal form.
	t.Log("confirmed: an inline interface literal parameter does not satisfy the assertion")
}

// Registration has to actually take effect for every channel that has one.
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

// An unregistered channel must still say so by name rather than resolve to an
// empty workspace, which would silently cross tenants.
func TestAnUnregisteredChannelIsRefusedByName(t *testing.T) {
	resolver := NewCampaignWorkspaceResolver(nil, nil)

	if _, err := resolver.GetEntryWorkspaceID("conv-1", "carrier-pigeon"); err == nil {
		t.Error("an unregistered channel must not resolve to an empty workspace")
	}
}
