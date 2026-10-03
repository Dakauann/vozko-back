package advertising

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
	mm "vozko/domain/metamessaging"
	webhook_usecase "vozko/usecases/webhook"
)

func entry(object, id string, changes ...*mm.Change) *mm.EntryEnvelope {
	return &mm.EntryEnvelope{Object: object, Entry: &mm.Entry{ID: id, Changes: changes}}
}

type recordedLeads struct{ events []ads.LeadgenEvent }

func (r *recordedLeads) HandleLeadgen(_ context.Context, e ads.LeadgenEvent) error {
	r.events = append(r.events, e)
	return nil
}

type recordedAccounts struct{ changes []ads.AdAccountChange }

func (r *recordedAccounts) HandleAccountChanges(_ context.Context, c []ads.AdAccountChange) error {
	r.changes = append(r.changes, c...)
	return nil
}

func TestLeadgenChangesBecomeEventsAndOtherFieldsAreIgnored(t *testing.T) {
	leads := &recordedLeads{}
	env := entry("page", "page-1",
		&mm.Change{Field: "feed", Value: json.RawMessage(`{"item":"post"}`)},
		&mm.Change{Field: "leadgen", Value: json.RawMessage(`{"leadgen_id":"444","form_id":"555","ad_id":"666","created_time":1782862110}`)},
	)
	if err := leadgenEntryHandler(leads)(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(leads.events) != 1 {
		t.Fatalf("events %+v", leads.events)
	}
	got := leads.events[0]
	if got.LeadgenID != "444" || got.FormID != "555" || got.AdID != "666" || got.PageID != "page-1" || got.CreatedAt.Unix() != 1782862110 {
		t.Fatalf("event %+v", got)
	}
}

func TestLeadgenWithoutIdsIsDropped(t *testing.T) {
	env := entry("page", "page-1", &mm.Change{Field: "leadgen", Value: json.RawMessage(`{"form_id":"555"}`)})
	err := leadgenEntryHandler(&recordedLeads{})(context.Background(), env)
	if !errors.Is(err, mm.ErrInvalidWebhookPayload) || classifyWebhookFailure(err) != webhook_usecase.DispositionDrop {
		t.Fatalf("err %v", err)
	}
}

func TestAdAccountChangesCarryTheAccountAndObject(t *testing.T) {
	accounts := &recordedAccounts{}
	env := entry("ad_account", "1234",
		&mm.Change{Field: "with_issues_ad_objects", Value: json.RawMessage(`{"id":"777","level":"AD","error_code":"567"}`)},
		&mm.Change{Field: "creative_fatigue", Value: json.RawMessage(`{}`)},
	)
	if err := accountEntryHandler(accounts)(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(accounts.changes) != 2 || accounts.changes[0].AccountMetaID != "1234" || accounts.changes[0].ObjectIDs[0] != "777" || accounts.changes[1].ObjectIDs != nil {
		t.Fatalf("changes %+v", accounts.changes)
	}
}

func TestWebhookFailuresRetryOnlyWhenMetaMightRecover(t *testing.T) {
	cases := map[ads.Failure]webhook_usecase.Disposition{
		ads.FailureRetryable:  webhook_usecase.DispositionRetry,
		ads.FailureReauth:     webhook_usecase.DispositionDrop,
		ads.FailurePermission: webhook_usecase.DispositionDrop,
		ads.FailureRejected:   webhook_usecase.DispositionDeadLetter,
	}
	for kind, want := range cases {
		if got := classifyWebhookFailure(&ads.RemoteError{Kind: kind}); got != want {
			t.Errorf("%s -> %v, want %v", kind, got, want)
		}
	}
	if classifyWebhookFailure(errors.New("database down")) != webhook_usecase.DispositionRetry {
		t.Fatal("local failures must retry")
	}
}
