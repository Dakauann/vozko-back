package scheduled_message

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestATemplateIsBoundOnlyByTheHorizon(t *testing.T) {
	cases := []struct {
		name      string
		open      bool
		expiresAt *time.Time
	}{
		{"a closed window", false, nil},
		{"a closed window with a countdown", false, at(6 * time.Hour)},
		{"an open window that closes soon", true, at(6 * time.Hour)},
		{"an open window with no clock", true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := KindTemplate.LatestAllowed(tc.open, tc.expiresAt, now)
			if err != nil {
				t.Fatalf("a template is sendable outside the window, got %v", err)
			}
			if !got.Equal(now.Add(MaxScheduleHorizon)) {
				t.Errorf("latest = %s, want the horizon %s", got, now.Add(MaxScheduleHorizon))
			}
		})
	}
}

func TestATextMessageKeepsTheWindowRule(t *testing.T) {
	if _, err := KindText.LatestAllowed(false, nil, now); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("err = %v, want a closed window to refuse free-form text", err)
	}
	got, err := KindText.LatestAllowed(true, at(6*time.Hour), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(now.Add(6 * time.Hour)) {
		t.Errorf("latest = %s, want the window close", got)
	}
}

func TestValidateScheduledAtForATemplate(t *testing.T) {
	windowIn6h := at(6 * time.Hour)

	cases := []struct {
		name      string
		at        time.Time
		open      bool
		expiresAt *time.Time
		wantErr   error
	}{
		{
			name: "while the window is closed",
			at:   now.Add(2 * time.Hour), open: false,
		},
		{
			name: "after the open window closes",
			at:   now.Add(48 * time.Hour), open: true, expiresAt: windowIn6h,
		},
		{
			name: "exactly at the horizon",
			at:   now.Add(MaxScheduleHorizon), open: false,
		},
		{
			name: "past the horizon is a horizon error, never a window error",
			at:   now.Add(MaxScheduleHorizon + time.Hour), open: true, expiresAt: windowIn6h,
			wantErr: ErrScheduledAtTooFar,
		},
		{
			name: "inside the minimum lead",
			at:   now.Add(MinScheduleLead - time.Nanosecond), open: false,
			wantErr: ErrScheduledAtTooSoon,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := KindTemplate.ValidateScheduledAt(tc.at, tc.open, tc.expiresAt, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func validTemplateMessage() *ScheduledMessage {
	return &ScheduledMessage{
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		CreatedByUserID: "user-1",
		Kind:            KindTemplate,
		Template: &TemplateContent{
			ID:         "tpl-1",
			Name:       "follow_up",
			BodyParams: []string{"Ana"},
		},
		ScheduledAt: now.Add(48 * time.Hour),
	}
}

func TestValidateATemplateMessage(t *testing.T) {
	mediaID := "med-1"
	replyID := "msg-1"

	cases := []struct {
		name    string
		mutate  func(*ScheduledMessage)
		wantErr error
	}{
		{"valid", func(*ScheduledMessage) {}, nil},
		{"no template", func(m *ScheduledMessage) { m.Template = nil }, ErrTemplateRequired},
		{"a blank template id", func(m *ScheduledMessage) { m.Template.ID = "  " }, ErrTemplateRequired},
		{"free text alongside", func(m *ScheduledMessage) { m.Text = "oi" }, ErrTemplateWithFreeContent},
		{"media alongside", func(m *ScheduledMessage) { m.MediaID = &mediaID }, ErrTemplateWithFreeContent},
		{"a quoted reply", func(m *ScheduledMessage) { m.ReplyToMessageID = &replyID }, ErrTemplateWithFreeContent},
		{"a signature", func(m *ScheduledMessage) { m.Signed = true }, ErrTemplateWithFreeContent},
		{"a channel without templates", func(m *ScheduledMessage) {
			m.EntryType = shared.EntryTypeInstagram
		}, ErrTemplatesUnsupported},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validTemplateMessage()
			tc.mutate(m)
			m.Normalize()
			if err := m.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestATextMessageCannotCarryATemplate(t *testing.T) {
	m := validMessage()
	m.Template = &TemplateContent{ID: "tpl-1"}
	m.Normalize()

	if err := m.Validate(); !errors.Is(err, ErrKindMismatch) {
		t.Fatalf("err = %v, want a text message carrying a template to be refused", err)
	}
}

func TestAnUnknownKindIsRefused(t *testing.T) {
	m := validMessage()
	m.Kind = "carrier-pigeon"
	m.Normalize()

	if err := m.Validate(); !errors.Is(err, ErrKindInvalid) {
		t.Fatalf("err = %v, want an unknown kind refused", err)
	}
}

func TestAMessageWithNoKindIsText(t *testing.T) {
	m := validMessage()
	m.Kind = ""
	m.Normalize()

	if m.Kind != KindText {
		t.Fatalf("kind = %q, want an unset kind to take the stricter text rules", m.Kind)
	}
	if !m.Kind.RequiresWindow() {
		t.Error("text must require an open window")
	}
	if KindTemplate.RequiresWindow() {
		t.Error("a template must not require an open window")
	}
}

func TestNormalizeTrimsTheTemplateID(t *testing.T) {
	m := validTemplateMessage()
	m.Template.ID = "  tpl-1  "
	m.Normalize()

	if m.Template.ID != "tpl-1" {
		t.Errorf("template id = %q, want it trimmed", m.Template.ID)
	}
}
