package instagram

import (
	"context"
	"errors"
	"testing"

	igdomain "vozko/domain/instagram"
)

var errConsent = errors.New("User consent is required to access user profile")

func enrichmentFixture(senders map[string]*igdomain.MessageSender) (*HandleWebhookUseCase, *fakeContactRepo, *fakeMessagingService) {
	contacts := &fakeContactRepo{}
	messaging := &fakeMessagingService{ProfileErr: errConsent, Senders: senders}
	uc := NewHandleWebhookUseCase(HandleWebhookDeps{Contacts: contacts, Messaging: messaging})
	return uc, contacts, messaging
}

var storyAccount = &igdomain.Account{ID: "acc", IGUserID: "biz", AccessToken: "token"}

func TestRefusedProfileFallsBackToTheMessageSenderUsername(t *testing.T) {
	uc, contacts, _ := enrichmentFixture(map[string]*igdomain.MessageSender{"mid-1": {ID: "igsid-1", Username: "maria.silva"}})
	uc.enrichContact(context.Background(), storyAccount, &igdomain.Contact{ID: "c1", IGSID: "igsid-1"}, "mid-1")
	if contacts.Usernames["c1"] != "maria.silva" {
		t.Fatalf("username = %q", contacts.Usernames["c1"])
	}
	if _, stamped := contacts.Profiles["c1"]; stamped {
		t.Fatal("the full profile must keep being retried until the person sends a DM")
	}
}

func TestSenderUsernameIsIgnoredWhenTheSenderIsSomeoneElse(t *testing.T) {
	uc, contacts, _ := enrichmentFixture(map[string]*igdomain.MessageSender{"mid-1": {ID: "other", Username: "intruder"}})
	uc.enrichContact(context.Background(), storyAccount, &igdomain.Contact{ID: "c1", IGSID: "igsid-1"}, "mid-1")
	if _, set := contacts.Usernames["c1"]; set {
		t.Fatal("a username from another participant must never be stored")
	}
}

func TestKnownUsernamesAreNotLookedUpAgain(t *testing.T) {
	uc, contacts, messaging := enrichmentFixture(nil)
	uc.enrichContact(context.Background(), storyAccount, &igdomain.Contact{ID: "c1", IGSID: "igsid-1", Username: "maria.silva"}, "mid-1")
	if len(messaging.SenderLookups) != 0 || len(contacts.Usernames) != 0 {
		t.Fatal("no lookup when the username is already known")
	}
}

func TestSenderLookupFailureLeavesTheContactUntouched(t *testing.T) {
	uc, contacts, _ := enrichmentFixture(nil)
	uc.enrichContact(context.Background(), storyAccount, &igdomain.Contact{ID: "c1", IGSID: "igsid-1"}, "mid-1")
	if len(contacts.Usernames) != 0 || len(contacts.Profiles) != 0 {
		t.Fatal("a failed lookup must not write anything")
	}
}

func TestGrantedProfileStillWins(t *testing.T) {
	uc, contacts, messaging := enrichmentFixture(nil)
	messaging.ProfileErr = nil
	uc.enrichContact(context.Background(), storyAccount, &igdomain.Contact{ID: "c1", IGSID: "igsid-1"}, "mid-1")
	if contacts.Profiles["c1"].Username != "someone" || len(messaging.SenderLookups) != 0 {
		t.Fatalf("profiles = %+v lookups = %v", contacts.Profiles, messaging.SenderLookups)
	}
}
