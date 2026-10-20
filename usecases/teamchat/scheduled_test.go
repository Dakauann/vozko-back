package teamchat_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/teamchat"
)

func (h *harness) schedule(t *testing.T, who, threadID, body string, in time.Duration) teamchat.Scheduled {
	t.Helper()
	s, err := h.svc.Schedule(context.Background(), as(who), testWorkspace, threadID, teamchat.Draft{Body: body}, h.clock.now.Add(in))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestScheduleWakesTheDispatcherAndListsOnlyMine(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dm := h.dm(t, ana, bia)
	s := h.schedule(t, ana, dm.Thread.ID, "lembrete", time.Hour)
	if !h.wake.fires[s.ID].Equal(s.SendAt) {
		t.Fatalf("wake = %v, want %v", h.wake.fires[s.ID], s.SendAt)
	}
	mine, _ := h.svc.Scheduled(ctx, as(ana), testWorkspace, dm.Thread.ID)
	theirs, _ := h.svc.Scheduled(ctx, as(bia), testWorkspace, dm.Thread.ID)
	if len(mine) != 1 || len(theirs) != 0 {
		t.Fatalf("mine = %d, theirs = %d", len(mine), len(theirs))
	}
	if _, err := h.svc.Schedule(ctx, as(ana), testWorkspace, dm.Thread.ID, teamchat.Draft{Body: "x"}, h.clock.now); !errors.Is(err, teamchat.ErrSendAtTooSoon) {
		t.Fatalf("now: %v", err)
	}
	if _, err := h.svc.Schedule(ctx, as(caio), testWorkspace, dm.Thread.ID, teamchat.Draft{Body: "x"}, h.clock.now.Add(time.Hour)); !errors.Is(err, teamchat.ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}
	if got := h.announcer.of("scheduled:pending"); len(got) != 1 || got[0].recipients[0] != ana {
		t.Fatalf("scheduled announcements = %+v", got)
	}
}

func TestRescheduleAndCancelBelongToTheAuthor(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dm := h.dm(t, ana, bia)
	s := h.schedule(t, ana, dm.Thread.ID, "lembrete", time.Hour)
	later := h.clock.now.Add(2 * time.Hour)
	if _, err := h.svc.Reschedule(ctx, as(bia), testWorkspace, s.ID, teamchat.Reschedule{SendAt: &later}); !errors.Is(err, teamchat.ErrScheduledNotFound) {
		t.Fatalf("bia reschedules: %v", err)
	}
	moved, err := h.svc.Reschedule(ctx, as(ana), testWorkspace, s.ID, teamchat.Reschedule{SendAt: &later})
	if err != nil || !moved.SendAt.Equal(later) || !h.wake.fires[s.ID].Equal(later) {
		t.Fatalf("moved = %+v, wake = %v, err = %v", moved, h.wake.fires[s.ID], err)
	}
	if err := h.svc.CancelScheduled(ctx, as(bia), testWorkspace, s.ID); !errors.Is(err, teamchat.ErrScheduledNotFound) {
		t.Fatalf("bia cancels: %v", err)
	}
	if err := h.svc.CancelScheduled(ctx, as(ana), testWorkspace, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.CancelScheduled(ctx, as(ana), testWorkspace, s.ID); !errors.Is(err, teamchat.ErrScheduledFinal) {
		t.Fatalf("twice: %v", err)
	}
}

func TestDispatchSendsOnceWhenDueAndIgnoresEarlyWakes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dm := h.dm(t, ana, bia)
	s := h.schedule(t, ana, dm.Thread.ID, "lembrete", time.Hour)

	if err := h.svc.DispatchScheduled(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if len(h.chats.textMessages(dm.Thread.ID)) != 0 {
		t.Fatal("an early wake sent the message")
	}
	h.tick(time.Hour)
	if err := h.svc.DispatchScheduled(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.DispatchScheduled(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	sent := h.chats.textMessages(dm.Thread.ID)
	if len(sent) != 1 || sent[0].ClientID != "scheduled:"+s.ID || sent[0].AuthorID != ana {
		t.Fatalf("sent = %+v", sent)
	}
	stored, _ := h.chats.FindScheduled(ctx, s.ID)
	if stored.Status != teamchat.ScheduledSent || stored.SentMessageID != sent[0].ID {
		t.Fatalf("stored = %+v", stored)
	}
	if len(h.announcer.of("message")) != 1 || len(h.announcer.of("scheduled:sent")) != 1 {
		t.Fatalf("announcements = %+v", h.announcer.sent)
	}
}

func TestADeliveryInterruptedAfterSendingNeverSendsAgain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dm := h.dm(t, ana, bia)
	s := h.schedule(t, ana, dm.Thread.ID, "lembrete", time.Hour)
	h.tick(time.Hour)
	if _, _, err := h.chats.Append(ctx, mustText(t, dm.Thread, s)); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.DispatchScheduled(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.chats.textMessages(dm.Thread.ID); len(got) != 1 {
		t.Fatalf("messages = %+v", got)
	}
}

func mustText(t *testing.T, thread teamchat.Thread, s teamchat.Scheduled) teamchat.Message {
	t.Helper()
	msg, err := teamchat.NewText("99999999-0000-4000-8000-000000000001", thread, s.AuthorID, s.Draft(), s.SendAt)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestDispatchFailsWithTheReasonWhenTheRulesNoLongerHold(t *testing.T) {
	cases := []struct {
		name   string
		change func(h *harness, threadID string)
		want   teamchat.FailureReason
	}{
		{"the author left the workspace", func(h *harness, _ string) { h.access.leave(ana) }, teamchat.ReasonLeftWorkspace},
		{"the recipient left the workspace", func(h *harness, _ string) { h.access.leave(bia) }, teamchat.ReasonRecipientUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			dm := h.dm(t, ana, bia)
			s := h.schedule(t, ana, dm.Thread.ID, "lembrete", time.Hour)
			tc.change(h, dm.Thread.ID)
			h.tick(time.Hour)
			if err := h.svc.DispatchScheduled(ctx, s.ID); err != nil {
				t.Fatal(err)
			}
			stored, _ := h.chats.FindScheduled(ctx, s.ID)
			if stored.Status != teamchat.ScheduledFailed || stored.FailureReason != tc.want {
				t.Fatalf("stored = %+v", stored)
			}
			if len(h.chats.textMessages(dm.Thread.ID)) != 0 {
				t.Fatal("a failed dispatch sent the message")
			}
		})
	}
}

func TestDispatchFailsWhenTheAuthorLeftTheGroup(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	view := h.group(t)
	s := h.schedule(t, bia, view.Thread.ID, "lembrete", time.Hour)
	if err := h.svc.Leave(ctx, as(bia), testWorkspace, view.Thread.ID); err != nil {
		t.Fatal(err)
	}
	h.tick(time.Hour)
	if err := h.svc.DispatchScheduled(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.chats.FindScheduled(ctx, s.ID)
	if stored.FailureReason != teamchat.ReasonNotMember {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestTheSweepDeliversEveryDueMessage(t *testing.T) {
	h := newHarness(t)
	dm := h.dm(t, ana, bia)
	h.schedule(t, ana, dm.Thread.ID, "um", time.Hour)
	h.schedule(t, bia, dm.Thread.ID, "dois", 2*time.Hour)
	h.schedule(t, ana, dm.Thread.ID, "tres", 5*time.Hour)
	h.tick(3 * time.Hour)
	if err := h.svc.SweepScheduled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.chats.textMessages(dm.Thread.ID); len(got) != 2 {
		t.Fatalf("delivered = %+v", got)
	}
}
