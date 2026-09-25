package shared

import "testing"

type entryAccessStub bool

func (a entryAccessStub) CanAccessEntry(_, _, _, _ string, _ bool) bool { return bool(a) }

func TestPersonMayActOnlyOnConversationsTheyCanSee(t *testing.T) {
	someone := Person{UserID: "u1"}
	if !someone.MayActOn(entryAccessStub(true), "ws1", "e1", "whatsapp") {
		t.Fatal("a visible conversation must be allowed")
	}
	if someone.MayActOn(entryAccessStub(false), "ws1", "e1", "whatsapp") {
		t.Fatal("an invisible conversation must be refused")
	}
	if someone.MayActOn(nil, "ws1", "e1", "instagram") {
		t.Fatal("no access check must refuse")
	}
	if (Person{}).MayActOn(entryAccessStub(true), "ws1", "e1", "whatsapp") {
		t.Fatal("nobody must be refused")
	}
}

func TestPersonMayNotActOnAnUnknownEntryType(t *testing.T) {
	if (Person{UserID: "u1"}).MayActOn(entryAccessStub(true), "ws1", "e1", "support") {
		t.Fatal("an entry type without conversation visibility must be refused")
	}
}
