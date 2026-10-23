package lead_usecase

import (
	"context"
	"slices"
	"testing"

	"vozko/domain/emailaddress"
	"vozko/domain/lead"
)

func TestALeadCreatedWithAnEmailStartsItsDomainCheck(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	if _, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Bia", Email: "Bia@Nova.com.br"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.seen.Seen(), []emailaddress.Address{"bia@nova.com.br"}) {
		t.Fatalf("seen %v, want the new lead's address", f.seen.Seen())
	}
}

func TestALeadCreatedWithoutAnEmailSeesNothing(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	if _, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Bia"}); err != nil {
		t.Fatal(err)
	}
	if len(f.seen.Seen()) != 0 {
		t.Fatalf("seen %v", f.seen.Seen())
	}
}

func TestOnlyALeadEditThatChangesTheEmailStartsItsDomainCheck(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	edited, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Nickname: text("Aninha")})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.seen.Seen()) != 0 {
		t.Fatalf("an edit that kept the email saw %v", f.seen.Seen())
	}
	if _, err := f.cmds.Update(context.Background(), operator(), "l-1", version(edited.Version), lead.Edit{Email: text("ana@nova.com.br")}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.seen.Seen(), []emailaddress.Address{"ana@nova.com.br"}) {
		t.Fatalf("seen %v, want the edited address", f.seen.Seen())
	}
}

func TestAProfileUpdateThatSetsTheEmailStartsItsDomainCheck(t *testing.T) {
	stored := storedLead()
	stored.Email = ""
	f := newProfileFixture(t, stored)
	if _, err := f.profiles.Update(context.Background(), conversationUpdate(lead.Profile{Email: "ana@nova.com.br"})); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.seen.Seen(), []emailaddress.Address{"ana@nova.com.br"}) {
		t.Fatalf("seen %v, want the address the conversation gave", f.seen.Seen())
	}
}
