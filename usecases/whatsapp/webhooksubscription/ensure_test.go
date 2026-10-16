package webhooksubscription_usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"vozko/domain/metaplatform"
)

type fakeGateway struct {
	subs       []metaplatform.AppSubscription
	posted     []metaplatform.AppSubscription
	keepFields bool
}

func (f *fakeGateway) AppSubscriptions(context.Context) ([]metaplatform.AppSubscription, error) {
	return slices.Clone(f.subs), nil
}

func (f *fakeGateway) SubscribeApp(_ context.Context, sub metaplatform.AppSubscription, _ string) error {
	f.posted = append(f.posted, sub)
	if f.keepFields {
		for i := range f.subs {
			if f.subs[i].Object == sub.Object {
				f.subs[i] = sub
			}
		}
	}
	return nil
}

func whatsappSub(fields ...string) metaplatform.AppSubscription {
	return metaplatform.AppSubscription{Object: "whatsapp_business_account", CallbackURL: "https://api.vozkoia.com/webhooks/whatsapp", Fields: fields, Active: true}
}

func TestEnsureAddsUserPreferencesWithoutDroppingAnyExistingField(t *testing.T) {
	gw := &fakeGateway{subs: []metaplatform.AppSubscription{whatsappSub("messages", "message_template_status_update")}, keepFields: true}

	changed, err := NewEnsureUseCase(gw, "verify").Ensure(context.Background())

	if err != nil || !changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if len(gw.posted) != 1 || !slices.Equal(gw.posted[0].Fields, []string{"messages", "message_template_status_update", "user_preferences"}) {
		t.Fatalf("posted %+v", gw.posted)
	}
	if gw.posted[0].CallbackURL != "https://api.vozkoia.com/webhooks/whatsapp" {
		t.Fatalf("the existing callback is kept, posted %q", gw.posted[0].CallbackURL)
	}
}

func TestEnsureDoesNothingWhenAlreadySubscribed(t *testing.T) {
	gw := &fakeGateway{subs: []metaplatform.AppSubscription{whatsappSub("messages", "user_preferences")}}

	changed, err := NewEnsureUseCase(gw, "verify").Ensure(context.Background())

	if err != nil || changed || len(gw.posted) != 0 {
		t.Fatalf("changed %v, err %v, posted %+v", changed, err, gw.posted)
	}
}

func TestEnsureNeverCreatesASubscriptionThatIsNotThere(t *testing.T) {
	gw := &fakeGateway{}

	_, err := NewEnsureUseCase(gw, "verify").Ensure(context.Background())

	if !errors.Is(err, metaplatform.ErrAppSubscriptionMissing) || len(gw.posted) != 0 {
		t.Fatalf("err %v, posted %+v", err, gw.posted)
	}
}

func TestEnsureFailsWhenMetaDoesNotKeepTheFields(t *testing.T) {
	gw := &fakeGateway{subs: []metaplatform.AppSubscription{whatsappSub("messages")}}

	if _, err := NewEnsureUseCase(gw, "verify").Ensure(context.Background()); !errors.Is(err, metaplatform.ErrAppSubscriptionNotKept) {
		t.Fatalf("want ErrAppSubscriptionNotKept, got %v", err)
	}
}
