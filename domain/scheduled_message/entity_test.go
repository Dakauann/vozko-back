package scheduled_message

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

var now = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time {
	t := now.Add(d)
	return &t
}

// LatestAllowed is the whole product rule in one function, so it gets the
// exhaustive table.
func TestLatestAllowed(t *testing.T) {
	cases := []struct {
		name      string
		open      bool
		expiresAt *time.Time
		want      time.Time
		wantErr   error
	}{
		{
			name:    "a closed window cannot be scheduled into",
			open:    false,
			wantErr: ErrWindowClosed,
		},
		{
			// THE case. The unofficial WhatsApp adapter reports an expiry while
			// CLOSED — it is the countdown on a provider restriction, not a
			// deadline to schedule up to. Reading it as a bound would let an
			// operator park a message on a number WhatsApp has restricted.
			name:      "a closed window with an expiry is still closed",
			open:      false,
			expiresAt: at(6 * time.Hour),
			wantErr:   ErrWindowClosed,
		},
		{
			// Telegram in bot mode, a healthy linked device: open, no clock.
			name: "an open window with no expiry is bounded only by the horizon",
			open: true,
			want: now.Add(MaxScheduleHorizon),
		},
		{
			name:      "the window bounds when it closes before the horizon",
			open:      true,
			expiresAt: at(6 * time.Hour),
			want:      now.Add(6 * time.Hour),
		},
		{
			name:      "the horizon bounds when the window outlasts it",
			open:      true,
			expiresAt: at(MaxScheduleHorizon + 24*time.Hour),
			want:      now.Add(MaxScheduleHorizon),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LatestAllowed(tc.open, tc.expiresAt, now)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("latest = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestValidateScheduledAt(t *testing.T) {
	windowIn6h := at(6 * time.Hour)

	cases := []struct {
		name      string
		at        time.Time
		open      bool
		expiresAt *time.Time
		wantErr   error
	}{
		{
			name: "comfortably inside an open window",
			at:   now.Add(2 * time.Hour), open: true, expiresAt: windowIn6h,
		},
		{
			name: "exactly at the window boundary is allowed",
			at:   now.Add(6 * time.Hour), open: true, expiresAt: windowIn6h,
		},
		{
			name: "one nanosecond past the window is not",
			at:   now.Add(6*time.Hour + time.Nanosecond), open: true, expiresAt: windowIn6h,
			wantErr: ErrScheduledAtPastWindow,
		},
		{
			name: "exactly at the minimum lead is allowed",
			at:   now.Add(MinScheduleLead), open: true, expiresAt: windowIn6h,
		},
		{
			name: "one nanosecond inside the minimum lead is not",
			at:   now.Add(MinScheduleLead - time.Nanosecond), open: true, expiresAt: windowIn6h,
			wantErr: ErrScheduledAtTooSoon,
		},
		{
			name: "the past is not a schedule",
			at:   now.Add(-time.Hour), open: true, expiresAt: windowIn6h,
			wantErr: ErrScheduledAtTooSoon,
		},
		{
			// A clockless channel refuses on the horizon, and must say so —
			// "past the window" would name a window that does not exist.
			name: "past the horizon on a clockless channel is a horizon error",
			at:   now.Add(MaxScheduleHorizon + time.Hour), open: true,
			wantErr: ErrScheduledAtTooFar,
		},
		{
			name: "a closed window refuses before any other check",
			at:   now.Add(2 * time.Hour), open: false, expiresAt: windowIn6h,
			wantErr: ErrWindowClosed,
		},
		{
			// Both bounds are breached; the window is the one the operator can
			// act on, so it wins the message.
			name: "a window breach outranks a horizon breach",
			at:   now.Add(MaxScheduleHorizon + time.Hour), open: true, expiresAt: windowIn6h,
			wantErr: ErrScheduledAtPastWindow,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateScheduledAt(tc.at, tc.open, tc.expiresAt, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// The state machine is what makes delivery at-most-once. A single wrong edge —
// sending back to pending — is a duplicate message to a paying customer.
func TestStatusTransitions(t *testing.T) {
	all := []Status{StatusPending, StatusSending, StatusSent, StatusFailed, StatusCanceled}
	allowed := map[Status]map[Status]bool{
		StatusPending: {StatusSending: true, StatusCanceled: true, StatusFailed: true},
		StatusSending: {StatusSent: true, StatusFailed: true},
	}

	for _, from := range all {
		for _, to := range all {
			want := allowed[from][to]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s -> %s = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTerminalStatusesAcceptNothing(t *testing.T) {
	for _, s := range []Status{StatusSent, StatusFailed, StatusCanceled} {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
		for _, to := range []Status{StatusPending, StatusSending, StatusSent, StatusFailed, StatusCanceled} {
			if s.CanTransitionTo(to) {
				t.Errorf("terminal %s accepted a transition to %s", s, to)
			}
		}
	}
	for _, s := range []Status{StatusPending, StatusSending} {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func validMessage() *ScheduledMessage {
	return &ScheduledMessage{
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		CreatedByUserID: "user-1",
		Text:            "oi",
		ScheduledAt:     now.Add(time.Hour),
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*ScheduledMessage)
		wantErr error
	}{
		{"valid", func(*ScheduledMessage) {}, nil},
		{"no workspace", func(m *ScheduledMessage) { m.WorkspaceID = " " }, ErrWorkspaceRequired},
		{"no entry", func(m *ScheduledMessage) { m.EntryID = "" }, ErrEntryIDRequired},
		{"unknown channel", func(m *ScheduledMessage) { m.EntryType = "carrier-pigeon" }, ErrEntryTypeInvalid},
		{"no sender", func(m *ScheduledMessage) { m.CreatedByUserID = "" }, ErrSenderRequired},
		{"no content", func(m *ScheduledMessage) { m.Text = "" }, ErrContentRequired},
		{"media alone is content", func(m *ScheduledMessage) {
			m.Text = ""
			id := "med-1"
			m.MediaID = &id
		}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validMessage()
			tc.mutate(m)
			m.Normalize()
			if err := m.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// A caller passing "" and a caller passing nil must produce the same row, or
// the same message scheduled from two surfaces stores differently.
func TestNormalizeCollapsesEmptyOptionals(t *testing.T) {
	blank := "   "
	m := validMessage()
	m.Text = "  oi  "
	m.MediaID = &blank
	m.ReplyToMessageID = &blank
	m.IdempotencyKey = &blank
	m.Normalize()

	if m.Text != "oi" {
		t.Errorf("text = %q, want it trimmed", m.Text)
	}
	if m.MediaID != nil || m.ReplyToMessageID != nil || m.IdempotencyKey != nil {
		t.Errorf("blank optionals survived normalization: %+v", m)
	}
	if m.Status != StatusPending {
		t.Errorf("status = %q, want a new message to start pending", m.Status)
	}
}

func TestIsDue(t *testing.T) {
	m := validMessage()
	m.ScheduledAt = now

	if !m.IsDue(now) {
		t.Error("a message scheduled for exactly now is due")
	}
	if !m.IsDue(now.Add(time.Second)) {
		t.Error("a message scheduled in the past is due")
	}
	if m.IsDue(now.Add(-time.Second)) {
		t.Error("a message scheduled for the future is not due yet")
	}
}
