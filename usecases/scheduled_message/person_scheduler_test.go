package scheduled_message_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type personAccess bool

func (a personAccess) CanAccessEntry(_, _, _, _ string, _ bool) bool { return bool(a) }

type scheduleStub struct{ inputs []sm.ScheduleInput }

func (s *scheduleStub) Execute(_ context.Context, in sm.ScheduleInput) (*sm.ScheduleResult, error) {
	s.inputs = append(s.inputs, in)
	return &sm.ScheduleResult{Message: &sm.ScheduledMessage{ID: "m1"}}, nil
}

type rescheduleStub struct{ calls int }

func (r *rescheduleStub) Execute(context.Context, sm.RescheduleInput) (*sm.ScheduleResult, error) {
	r.calls++
	return &sm.ScheduleResult{}, nil
}

type cancelStub struct{ calls int }

func (c *cancelStub) Execute(context.Context, string, string) error {
	c.calls++
	return nil
}

func schedulerFixture(allow bool) (sm.PersonSchedulerUseCase, *scheduleStub, *rescheduleStub, *cancelStub) {
	uc, schedule, reschedule, cancel, _ := schedulerFixtureWith(allow, &fakePermissions{})
	return uc, schedule, reschedule, cancel
}

func schedulerFixtureWith(allow bool, permissions *fakePermissions) (sm.PersonSchedulerUseCase, *scheduleStub, *rescheduleStub, *cancelStub, *fakePermissions) {
	repo := newFakeRepo()
	repo.put(&sm.ScheduledMessage{ID: "m1", WorkspaceID: "ws1", EntryID: "e1", EntryType: shared.EntryTypeWhatsApp})
	repo.put(&sm.ScheduledMessage{ID: "m2", WorkspaceID: "ws2", EntryID: "e2", EntryType: shared.EntryTypeWhatsApp})
	repo.put(&sm.ScheduledMessage{ID: "t1", WorkspaceID: "ws1", EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, Kind: sm.KindTemplate})
	schedule, reschedule, cancel := &scheduleStub{}, &rescheduleStub{}, &cancelStub{}
	uc := NewPersonSchedulerUseCase(personAccess(allow), permissions, repo, schedule, reschedule, cancel)
	return uc, schedule, reschedule, cancel, permissions
}

func TestPersonSchedulerRefusesAConversationThePersonCannotSee(t *testing.T) {
	uc, schedule, reschedule, cancel := schedulerFixture(false)
	someone := shared.Person{UserID: "u1"}
	if _, err := uc.Schedule(context.Background(), someone, sm.ScheduleInput{WorkspaceID: "ws1", EntryID: "e1", EntryType: "whatsapp"}); !errors.Is(err, sm.ErrEntryAccess) {
		t.Fatalf("schedule: %v", err)
	}
	if _, err := uc.Reschedule(context.Background(), someone, sm.RescheduleInput{ID: "m1", WorkspaceID: "ws1", ScheduledAt: time.Now()}); !errors.Is(err, sm.ErrEntryAccess) {
		t.Fatalf("reschedule: %v", err)
	}
	if err := uc.Cancel(context.Background(), someone, "ws1", "m1"); !errors.Is(err, sm.ErrEntryAccess) {
		t.Fatalf("cancel: %v", err)
	}
	if len(schedule.inputs)+reschedule.calls+cancel.calls != 0 {
		t.Fatal("changed a schedule without access")
	}
}

func TestPersonSchedulerHidesAnotherWorkspacesMessage(t *testing.T) {
	uc, _, _, cancel := schedulerFixture(true)
	if err := uc.Cancel(context.Background(), shared.Person{UserID: "u1"}, "ws1", "m2"); !errors.Is(err, sm.ErrNotFound) || cancel.calls != 0 {
		t.Fatalf("err %v cancels %d", err, cancel.calls)
	}
}

func TestPersonSchedulerRecordsTheRealSender(t *testing.T) {
	uc, schedule, _, cancel := schedulerFixture(true)
	someone := shared.Person{UserID: "u1"}
	if _, err := uc.Schedule(context.Background(), someone, sm.ScheduleInput{WorkspaceID: "ws1", EntryID: "e1", EntryType: "whatsapp", CreatedByUserID: "forged"}); err != nil {
		t.Fatal(err)
	}
	if schedule.inputs[0].CreatedByUserID != "u1" {
		t.Fatalf("sender = %q", schedule.inputs[0].CreatedByUserID)
	}
	if err := uc.Cancel(context.Background(), someone, "ws1", "m1"); err != nil || cancel.calls != 1 {
		t.Fatalf("cancel err %v calls %d", err, cancel.calls)
	}
}

func templateSchedule() sm.ScheduleInput {
	return sm.ScheduleInput{
		WorkspaceID: "ws1", EntryID: "e1", EntryType: "whatsapp",
		Template: &sm.TemplateContent{ID: "tpl-1"},
	}
}

func TestPersonSchedulerRequiresTheTemplatePermissionForATemplate(t *testing.T) {
	uc, schedule, reschedule, cancel, _ := schedulerFixtureWith(true, &fakePermissions{err: workspace.ErrInsufficientPermissions})
	someone := shared.Person{UserID: "u1"}

	if _, err := uc.Schedule(context.Background(), someone, templateSchedule()); !errors.Is(err, sm.ErrTemplatePermission) {
		t.Fatalf("schedule: %v", err)
	}
	if _, err := uc.Reschedule(context.Background(), someone, sm.RescheduleInput{ID: "t1", WorkspaceID: "ws1", ScheduledAt: time.Now()}); !errors.Is(err, sm.ErrTemplatePermission) {
		t.Fatalf("reschedule: %v", err)
	}
	if err := uc.Cancel(context.Background(), someone, "ws1", "t1"); !errors.Is(err, sm.ErrTemplatePermission) {
		t.Fatalf("cancel: %v", err)
	}
	if len(schedule.inputs)+reschedule.calls+cancel.calls != 0 {
		t.Fatal("a paid template was changed by someone who may not send templates")
	}
}

func TestPersonSchedulerLeavesFreeTextToConversationAccess(t *testing.T) {
	uc, schedule, _, cancel, permissions := schedulerFixtureWith(true, &fakePermissions{err: workspace.ErrInsufficientPermissions})
	someone := shared.Person{UserID: "u1"}

	if _, err := uc.Schedule(context.Background(), someone, sm.ScheduleInput{WorkspaceID: "ws1", EntryID: "e1", EntryType: "whatsapp", Text: "oi"}); err != nil {
		t.Fatalf("schedule text: %v", err)
	}
	if err := uc.Cancel(context.Background(), someone, "ws1", "m1"); err != nil {
		t.Fatalf("cancel text: %v", err)
	}
	if len(schedule.inputs) != 1 || cancel.calls != 1 || permissions.calls() != 0 {
		t.Fatalf("free text must not ask for the template permission (checks %d)", permissions.calls())
	}
}

func TestPersonSchedulerChecksTheTemplatePermissionInTheRightWorkspace(t *testing.T) {
	uc, schedule, _, _, permissions := schedulerFixtureWith(true, &fakePermissions{})

	if _, err := uc.Schedule(context.Background(), shared.Person{UserID: "u1"}, templateSchedule()); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	want := "u1|ws1|" + string(workspace.ResourceWhatsAppTemplates) + ":" + string(workspace.ActionSend)
	if len(permissions.checked) != 1 || permissions.checked[0] != want {
		t.Fatalf("checks = %v, want %q", permissions.checked, want)
	}
	if len(schedule.inputs) != 1 {
		t.Fatal("an allowed template was not scheduled")
	}
}

func TestPersonSchedulerLetsASystemAdminScheduleATemplate(t *testing.T) {
	uc, schedule, _, _, permissions := schedulerFixtureWith(true, &fakePermissions{err: workspace.ErrUnauthorized})
	admin := shared.Person{UserID: "root", SystemAdmin: true}

	if _, err := uc.Schedule(context.Background(), admin, templateSchedule()); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(schedule.inputs) != 1 || permissions.calls() != 0 {
		t.Fatal("a system admin passes the route guard and must pass here too")
	}
}

func TestPersonSchedulerDoesNotTreatAnUnreadablePermissionAsGranted(t *testing.T) {
	uc, schedule, _, _, _ := schedulerFixtureWith(true, &fakePermissions{err: errors.New("db down")})

	if _, err := uc.Schedule(context.Background(), shared.Person{UserID: "u1"}, templateSchedule()); err == nil {
		t.Fatal("an unreadable permission let a template through")
	}
	if len(schedule.inputs) != 0 {
		t.Fatal("scheduled without a readable permission")
	}
}
