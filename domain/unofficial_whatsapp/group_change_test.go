package unofficial_whatsapp

import (
	"reflect"
	"testing"
)

func decodeBody(t *testing.T, body string) *Envelope {
	t.Helper()
	env, err := DecodeEnvelope([]byte(body))
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	return env
}

func TestAGroupsWebhookCarriesWhoLeftAndWhoActed(t *testing.T) {
	env := decodeBody(t, `{
		"EventType": "groups", "owner": "5511999999999", "type": "Group",
		"sender_pn": "5511777777777@s.whatsapp.net", "sender_lid": "99887766@lid",
		"event": {
			"JID": "120363000000000001@g.us",
			"Sender": "99887766@lid",
			"Leave": ["5511888888888@s.whatsapp.net", "123456789@lid"],
			"Join": [], "Promote": null, "Demote": []
		}
	}`)
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", env))

	if ev.ChatID != "120363000000000001@g.us" {
		t.Errorf("chat id = %q, want the group jid from event.JID", ev.ChatID)
	}
	wantLeft := []string{"5511888888888@s.whatsapp.net", "123456789@lid"}
	if !reflect.DeepEqual(ev.GroupChange.Left, wantLeft) {
		t.Errorf("left = %v, want %v", ev.GroupChange.Left, wantLeft)
	}
	wantActors := []string{"99887766@lid", "5511777777777@s.whatsapp.net"}
	if !reflect.DeepEqual(ev.GroupChange.Actors, wantActors) {
		t.Errorf("actors = %v, want %v", ev.GroupChange.Actors, wantActors)
	}
}

func TestParticipantObjectsInTheLeaveListKeepEveryProjection(t *testing.T) {
	change := ParseGroupParticipantChange([]byte(`{"event": {
		"Sender": "5511777777777@s.whatsapp.net",
		"Leave": [{"JID": "123456789@lid", "PhoneNumber": "5511888888888@s.whatsapp.net"}]
	}}`))
	want := []string{"123456789@lid", "5511888888888@s.whatsapp.net"}
	if !reflect.DeepEqual(change.Left, want) {
		t.Errorf("left = %v, want %v", change.Left, want)
	}
}

func TestAMetadataChangeHasNoDepartures(t *testing.T) {
	change := ParseGroupParticipantChange([]byte(`{"event": {
		"JID": "120363000000000001@g.us", "Sender": "5511888888888@s.whatsapp.net",
		"Name": {"Name": "Equipe"}, "Join": [], "Leave": [], "Promote": [], "Demote": []
	}}`))
	if !change.Empty() {
		t.Errorf("a rename must carry no departures, got %+v", change)
	}
}

func TestAnUnreadablePayloadIsAnEmptyChange(t *testing.T) {
	for _, body := range []string{``, `[]`, `{"event": "x"}`, `{"event": {"Leave": "5511@s.whatsapp.net"}}`} {
		if change := ParseGroupParticipantChange([]byte(body)); !change.Empty() || len(change.Actors) != 0 {
			t.Errorf("body %q gave %+v, want nothing", body, change)
		}
	}
}
