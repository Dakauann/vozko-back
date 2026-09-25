package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type entryBuilderStub struct{ built []string }

func (b *entryBuilderStub) BuildInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error) {
	b.built = append(b.built, entryID)
	return &conversation.InboxEntry{EntryID: entryID, EntryType: entryType, LeadName: "Maria"}, nil
}

func TestLookupEntryShowsNothingTheViewerCannotSee(t *testing.T) {
	builder := &entryBuilderStub{}
	_, err := NewEntryLookup(&accessStub{allow: false}, builder).LookupEntry(conversation.Viewer{UserID: "u1", WorkspaceID: "ws1"}, "e1", shared.EntryTypeWhatsApp)
	if !errors.Is(err, conversation.ErrUnauthorized) || len(builder.built) != 0 {
		t.Fatalf("err = %v, built %v", err, builder.built)
	}
}

func TestLookupEntryFailsClosedWithoutAnAuthorizer(t *testing.T) {
	_, err := NewEntryLookup(nil, &entryBuilderStub{}).LookupEntry(conversation.Viewer{UserID: "u1", WorkspaceID: "ws1"}, "e1", shared.EntryTypeWhatsApp)
	if !errors.Is(err, conversation.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestLookupEntryBuildsAVisibleEntry(t *testing.T) {
	entry, err := NewEntryLookup(&accessStub{allow: true}, &entryBuilderStub{}).LookupEntry(conversation.Viewer{UserID: "u1", WorkspaceID: "ws1"}, "e1", shared.EntryTypeWhatsApp)
	if err != nil || entry.LeadName != "Maria" {
		t.Fatalf("entry %+v err %v", entry, err)
	}
}
