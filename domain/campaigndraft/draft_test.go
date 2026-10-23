package campaigndraft

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func TestNewKeepsWhatIsFilledIn(t *testing.T) {
	d, err := New("d1", "w1", "u1", ChannelWhatsApp, "  Black Friday  ", json.RawMessage(`{"request":{"name":"Black Friday"},"state":{"step":"message"}}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Black Friday" || d.Channel != ChannelWhatsApp || d.WorkspaceID != "w1" || d.CreatedBy != "u1" {
		t.Fatalf("draft %+v", d)
	}
	if !d.CreatedAt.Equal(now) || !d.UpdatedAt.Equal(now) {
		t.Fatalf("times %v %v", d.CreatedAt, d.UpdatedAt)
	}
}

func TestNewAcceptsAnEmptyDraft(t *testing.T) {
	d, err := New("d1", "w1", "u1", ChannelUnofficialWhatsApp, "", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(d.Payload) != "{}" {
		t.Fatalf("payload %s", d.Payload)
	}
}

func TestNewRefusesAnUnknownChannel(t *testing.T) {
	if _, err := New("d1", "w1", "u1", Channel("email"), "", nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestNewNeedsAWorkspaceAndAnAuthor(t *testing.T) {
	if _, err := New("d1", "", "u1", ChannelWhatsApp, "", nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("workspace: %v", err)
	}
	if _, err := New("d1", "w1", "", ChannelWhatsApp, "", nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("author: %v", err)
	}
}

func TestPayloadMustBeAJSONObject(t *testing.T) {
	for _, raw := range []string{`[]`, `"x"`, `12`, `{"request":`, `null`} {
		if _, err := New("d1", "w1", "u1", ChannelWhatsApp, "", json.RawMessage(raw), now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: got %v", raw, err)
		}
	}
}

func TestPayloadHasACeiling(t *testing.T) {
	big := json.RawMessage(`{"state":"` + strings.Repeat("a", MaxPayloadBytes) + `"}`)
	if _, err := New("d1", "w1", "u1", ChannelWhatsApp, "", big, now); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestNameHasACeiling(t *testing.T) {
	if _, err := New("d1", "w1", "u1", ChannelWhatsApp, strings.Repeat("n", MaxNameLength+1), nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestReviseReplacesNameAndPayload(t *testing.T) {
	d, _ := New("d1", "w1", "u1", ChannelWhatsApp, "a", nil, now)
	later := now.Add(time.Minute)
	if err := d.Revise("b", json.RawMessage(`{"request":{"name":"b"}}`), later); err != nil {
		t.Fatal(err)
	}
	if d.Name != "b" || !d.UpdatedAt.Equal(later) || !d.CreatedAt.Equal(now) {
		t.Fatalf("draft %+v", d)
	}
	if err := d.Revise("c", json.RawMessage(`[]`), later); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
	if d.Name != "b" {
		t.Fatal("a refused revision changed the draft")
	}
}

func TestRequestReadsTheChannelRequest(t *testing.T) {
	d, _ := New("d1", "w1", "u1", ChannelWhatsApp, "", json.RawMessage(`{"request":{"name":"x"},"state":{}}`), now)
	raw, err := d.Request()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"name":"x"}` {
		t.Fatalf("request %s", raw)
	}
	empty, _ := New("d2", "w1", "u1", ChannelWhatsApp, "", nil, now)
	if _, err := empty.Request(); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("got %v", err)
	}
	wrong, _ := New("d3", "w1", "u1", ChannelWhatsApp, "", json.RawMessage(`{"request":[1]}`), now)
	if _, err := wrong.Request(); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("array: got %v", err)
	}
}

func TestErrorCodes(t *testing.T) {
	cases := map[error]string{
		ErrNotFound:   "campaign_draft_not_found",
		ErrInvalid:    "campaign_draft_invalid",
		ErrTooLarge:   "campaign_draft_too_large",
		ErrIncomplete: "campaign_draft_incomplete",
	}
	for err, code := range cases {
		if ErrorCode(err) != code {
			t.Fatalf("%v: %s", err, ErrorCode(err))
		}
	}
	if ErrorCode(errors.New("other")) != "" {
		t.Fatal("unknown error has a code")
	}
}

func TestParseChannel(t *testing.T) {
	if ch, err := ParseChannel("unofficial_whatsapp"); err != nil || ch != ChannelUnofficialWhatsApp {
		t.Fatalf("%v %v", ch, err)
	}
	if _, err := ParseChannel("sms"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}
