package scheduled_message_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	wo "vozko/domain/whatsapp_outreach"
)

type scheduleFixture struct {
	repo      *fakeRepo
	windows   *fakeWindows
	templates *fakeTemplates
	wake      *fakeWake
	clock     *fixedClock
	uc        sm.ScheduleUseCase
}

func newScheduleFixture(t *testing.T) *scheduleFixture {
	t.Helper()
	expires := fixedNow.Add(6 * time.Hour)
	f := &scheduleFixture{
		repo:      newFakeRepo(),
		windows:   &fakeWindows{open: true, expiresAt: &expires},
		templates: &fakeTemplates{},
		wake:      &fakeWake{},
		clock:     &fixedClock{now: fixedNow},
	}
	uc, err := NewScheduleUseCase(f.repo, f.windows, f.templates, f.wake, f.clock)
	if err != nil {
		t.Fatalf("NewScheduleUseCase: %v", err)
	}
	f.uc = uc
	return f
}

func scheduleInput() sm.ScheduleInput {
	return sm.ScheduleInput{
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       string(shared.EntryTypeWhatsApp),
		CreatedByUserID: "user-1",
		Text:            "oi",
		ScheduledAt:     fixedNow.Add(2 * time.Hour),
	}
}

func TestScheduleStoresAndEnqueues(t *testing.T) {
	f := newScheduleFixture(t)

	result, err := f.uc.Execute(context.Background(), scheduleInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Message == nil || result.Message.Status != sm.StatusPending {
		t.Fatalf("result = %+v", result)
	}
	if result.AlreadyExisted {
		t.Error("a fresh schedule was reported as a replay")
	}
	if result.Message.WindowExpiresAtAtCreation == nil {
		t.Error("the window expiry was not recorded")
	}
	if result.Window.LatestAllowedAt == nil {
		t.Error("the result did not carry the boundary the client needs")
	}
	if f.wake.count() != 1 {
		t.Errorf("fires enqueued = %d, want 1", f.wake.count())
	}
}

func TestScheduleSucceedsWhenTheQueueIsDown(t *testing.T) {
	f := newScheduleFixture(t)
	f.wake.err = errors.New("broker unreachable")

	result, err := f.uc.Execute(context.Background(), scheduleInput())
	if err != nil {
		t.Fatalf("a dead broker must not fail the schedule: %v", err)
	}
	if result.Message.Status != sm.StatusPending {
		t.Errorf("status = %q, want pending so the sweep collects it", result.Message.Status)
	}
	if f.repo.get(result.Message.ID) == nil {
		t.Error("the message was not persisted")
	}
}

func TestScheduleIsIdempotentPerKey(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.IdempotencyKey = "key-1"

	first, err := f.uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := f.uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if second.Message.ID != first.Message.ID {
		t.Fatalf("a retried create produced a second message: %s vs %s", second.Message.ID, first.Message.ID)
	}
	if !second.AlreadyExisted {
		t.Error("the replay was not reported as one, so the caller would answer 201")
	}
	if f.wake.count() != 1 {
		t.Errorf("fires enqueued = %d, want 1: a replay must not double-schedule", f.wake.count())
	}
}

func TestScheduleWithoutAKeyCreatesEachTime(t *testing.T) {
	f := newScheduleFixture(t)

	first, _ := f.uc.Execute(context.Background(), scheduleInput())
	second, _ := f.uc.Execute(context.Background(), scheduleInput())

	if first.Message.ID == second.Message.ID {
		t.Fatal("two keyless creates collapsed into one")
	}
}

func TestScheduleRefusesAClosedWindow(t *testing.T) {
	f := newScheduleFixture(t)
	f.windows.set(false, nil)

	result, err := f.uc.Execute(context.Background(), scheduleInput())
	if !errors.Is(err, sm.ErrWindowClosed) {
		t.Fatalf("err = %v, want ErrWindowClosed", err)
	}
	if result == nil || result.Window.Open {
		t.Error("the refusal did not carry the window state the UI must explain")
	}
	if f.wake.count() != 0 {
		t.Error("a refused schedule still enqueued a fire")
	}
}

func TestScheduleRefusesPastTheWindowAndReportsTheBoundary(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.ScheduledAt = fixedNow.Add(8 * time.Hour)

	result, err := f.uc.Execute(context.Background(), in)
	if !errors.Is(err, sm.ErrScheduledAtPastWindow) {
		t.Fatalf("err = %v, want ErrScheduledAtPastWindow", err)
	}
	if result.Window.LatestAllowedAt == nil || !result.Window.LatestAllowedAt.Equal(fixedNow.Add(6*time.Hour)) {
		t.Errorf("latest allowed = %v, want the window's expiry", result.Window.LatestAllowedAt)
	}
}

func TestScheduleRefusesTooSoon(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.ScheduledAt = fixedNow.Add(10 * time.Second)

	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrScheduledAtTooSoon) {
		t.Fatalf("err = %v, want ErrScheduledAtTooSoon", err)
	}
}

func TestScheduleOnAClocklessChannelIsBoundedByTheHorizon(t *testing.T) {
	f := newScheduleFixture(t)
	f.windows.set(true, nil)

	in := scheduleInput()
	in.ScheduledAt = fixedNow.Add(20 * 24 * time.Hour)

	result, err := f.uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("a clockless channel refused a schedule inside the horizon: %v", err)
	}
	if result.Message.WindowExpiresAtAtCreation != nil {
		t.Error("a window expiry was invented for a channel that has none")
	}

	in.ScheduledAt = fixedNow.Add(sm.MaxScheduleHorizon + time.Hour)
	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrScheduledAtTooFar) {
		t.Fatalf("err = %v, want ErrScheduledAtTooFar", err)
	}
}

func TestScheduleValidatesContent(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.Text = "   "

	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrContentRequired) {
		t.Fatalf("err = %v, want ErrContentRequired", err)
	}
	if f.repo.get("") != nil {
		t.Error("an empty message was persisted")
	}
}

func TestScheduleAcceptsMediaWithoutText(t *testing.T) {
	f := newScheduleFixture(t)

	in := scheduleInput()
	in.Text = ""
	in.MediaID = "med-1"
	in.MediaType = "image"

	if _, err := f.uc.Execute(context.Background(), in); err != nil {
		t.Fatalf("a media-only schedule was refused: %v", err)
	}
}

func TestNewScheduleUseCaseRefusesMissingDependencies(t *testing.T) {
	repo, windows, templates, wake, clock := newFakeRepo(), &fakeWindows{}, &fakeTemplates{}, &fakeWake{}, &fixedClock{}

	if _, err := NewScheduleUseCase(nil, windows, templates, wake, clock); err == nil {
		t.Error("a nil repository was accepted")
	}
	if _, err := NewScheduleUseCase(repo, nil, templates, wake, clock); err == nil {
		t.Error("a nil window reader was accepted")
	}
	if _, err := NewScheduleUseCase(repo, windows, nil, wake, clock); err == nil {
		t.Error("a nil template check was accepted: templates would be scheduled unchecked")
	}
	if _, err := NewScheduleUseCase(repo, windows, templates, nil, clock); err == nil {
		t.Error("a nil wake scheduler was accepted: every schedule would silently be up to a minute late")
	}
	if _, err := NewScheduleUseCase(repo, windows, templates, wake, nil); err == nil {
		t.Error("a nil clock was accepted")
	}
}

func templateInput() sm.ScheduleInput {
	return sm.ScheduleInput{
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       string(shared.EntryTypeWhatsApp),
		CreatedByUserID: "user-1",
		Template:        &sm.TemplateContent{ID: "tpl-1", BodyParams: []string{"Ana"}},
		ScheduledAt:     fixedNow.Add(48 * time.Hour),
	}
}

func TestScheduleATemplateWhileTheWindowIsClosed(t *testing.T) {
	f := newScheduleFixture(t)
	f.windows.set(false, nil)

	result, err := f.uc.Execute(context.Background(), templateInput())
	if err != nil {
		t.Fatalf("a template must be schedulable outside the window: %v", err)
	}
	stored := f.repo.get(result.Message.ID)
	if stored.Kind != sm.KindTemplate || stored.Template == nil {
		t.Fatalf("stored = %+v, want a template message", stored)
	}
	if stored.Template.Name != "follow_up" || stored.Template.Preview != "Oi Ana, tudo certo?" {
		t.Errorf("the panel needs the template's name and preview, got %+v", stored.Template)
	}
	if result.Window.TemplateLatestAllowedAt == nil {
		t.Error("the result must carry the template bound the dialog uses")
	}
	if f.wake.count() != 1 {
		t.Errorf("fires enqueued = %d, want 1", f.wake.count())
	}

	check := f.templates.checks[0]
	if check.EntryID != "entry-1" || check.UserID != "user-1" || check.TemplateID != "tpl-1" || check.BodyParams[0] != "Ana" {
		t.Errorf("the template was checked against the wrong send: %+v", check)
	}
	if len(f.templates.sends) != 0 {
		t.Error("scheduling must never send or charge")
	}
}

func TestScheduleATemplateAfterTheWindowCloses(t *testing.T) {
	f := newScheduleFixture(t)

	if _, err := f.uc.Execute(context.Background(), templateInput()); err != nil {
		t.Fatalf("a template past the open window's close must be accepted: %v", err)
	}
}

func TestAFreeTextScheduleStillNeedsTheWindow(t *testing.T) {
	f := newScheduleFixture(t)
	f.windows.set(false, nil)

	if _, err := f.uc.Execute(context.Background(), scheduleInput()); !errors.Is(err, sm.ErrWindowClosed) {
		t.Fatalf("err = %v, want free text refused on a closed window", err)
	}
}

func TestScheduleATemplateTheSendRulesRefuse(t *testing.T) {
	f := newScheduleFixture(t)
	f.templates.checkErr = wo.ErrWithinSpamWindow

	if _, err := f.uc.Execute(context.Background(), templateInput()); !errors.Is(err, wo.ErrWithinSpamWindow) {
		t.Fatalf("err = %v, want the send rule's refusal surfaced", err)
	}
	if f.wake.count() != 0 || len(f.repo.messages) != 0 {
		t.Error("a refused template must not be stored or enqueued")
	}
}

func TestScheduleATemplateOnAChannelWithoutTemplates(t *testing.T) {
	f := newScheduleFixture(t)
	in := templateInput()
	in.EntryType = string(shared.EntryTypeInstagram)

	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrTemplatesUnsupported) {
		t.Fatalf("err = %v, want ErrTemplatesUnsupported", err)
	}
	if len(f.templates.checks) != 0 {
		t.Error("the template must not be checked on a channel that cannot send it")
	}
}

func TestScheduleATemplateBeyondTheHorizon(t *testing.T) {
	f := newScheduleFixture(t)
	in := templateInput()
	in.ScheduledAt = fixedNow.Add(sm.MaxScheduleHorizon + time.Hour)

	if _, err := f.uc.Execute(context.Background(), in); !errors.Is(err, sm.ErrScheduledAtTooFar) {
		t.Fatalf("err = %v, want ErrScheduledAtTooFar", err)
	}
}
