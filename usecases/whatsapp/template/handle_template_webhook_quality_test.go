package template_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
)

type fakeQualityRecorder struct {
	changes []quality.Change
	err     error
}

func (f *fakeQualityRecorder) Record(changes ...quality.Change) error {
	if f.err != nil {
		return f.err
	}
	f.changes = append(f.changes, changes...)
	return nil
}

func signalPayload(field string, at int64, v template.TemplateWebhookValue) *template.TemplateWebhookPayload {
	return &template.TemplateWebhookPayload{Entry: []template.TemplateWebhookEntry{{
		ID: "waba1", Time: at, Changes: []template.TemplateWebhookChange{{Field: field, Value: v}},
	}}}
}

func TestQualityUpdateIsRecordedAtTheMomentMetaReports(t *testing.T) {
	recorder := &fakeQualityRecorder{}
	uc := NewHandleTemplateWebhook(newMockTemplateRepo(), recorder)

	payload := signalPayload(template.FieldMessageTemplateQualityUpdate, 1791990878, template.TemplateWebhookValue{
		MessageTemplateID: 2096955574574516, MessageTemplateName: "refiliaxao_taxa_zero_correta",
		PreviousQualityScore: "YELLOW", NewQualityScore: "RED",
	})
	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(recorder.changes) != 1 {
		t.Fatalf("want one change, got %d", len(recorder.changes))
	}
	got := recorder.changes[0]
	want := quality.Change{
		Subject: quality.SubjectTemplate, ExternalID: "2096955574574516", WABAID: "waba1", Name: "refiliaxao_taxa_zero_correta",
		Signal: quality.SignalQuality, Previous: "YELLOW", Current: "RED", Source: quality.SourceWebhook,
		OccurredAt: time.Unix(1791990878, 0).UTC(),
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestStatusAndCategoryUpdatesAreRecordedButComponentsAreNot(t *testing.T) {
	recorder := &fakeQualityRecorder{}
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "2426430047838385", "waba1", "todas_as_reguas", template.TemplateStatusApproved, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo, recorder)

	for _, payload := range []*template.TemplateWebhookPayload{
		signalPayload(template.FieldMessageTemplateStatusUpdate, 1791986661, template.TemplateWebhookValue{MessageTemplateID: 2426430047838385, Event: "paused"}),
		signalPayload(template.FieldTemplateCategoryUpdate, 1791986700, template.TemplateWebhookValue{MessageTemplateID: 2426430047838385, PreviousCategory: "utility", NewCategory: "MARKETING"}),
		signalPayload(template.FieldMessageTemplateComponentsUpdate, 1791986800, template.TemplateWebhookValue{MessageTemplateID: 2426430047838385}),
	} {
		if err := uc.Execute(payload); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}

	if len(recorder.changes) != 2 {
		t.Fatalf("want status and category, got %+v", recorder.changes)
	}
	if c := recorder.changes[0]; c.Signal != quality.SignalStatus || c.Current != "PAUSED" || c.Event != "PAUSED" {
		t.Fatalf("status change %+v", c)
	}
	if c := recorder.changes[1]; c.Signal != quality.SignalCategory || c.Previous != "UTILITY" || c.Current != "MARKETING" {
		t.Fatalf("category change %+v", c)
	}
	if repo.templates["t1"].Status != template.TemplateStatusPaused {
		t.Fatal("recording must not replace the template update")
	}
}

func TestAFailedRecordingIsReturnedSoTheEventIsRetried(t *testing.T) {
	boom := errors.New("db down")
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "1902934413754004", "waba1", "regua_2oumais", template.TemplateStatusApproved, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo, &fakeQualityRecorder{err: boom})

	err := uc.Execute(signalPayload(template.FieldMessageTemplateStatusUpdate, 1791986661, template.TemplateWebhookValue{MessageTemplateID: 1902934413754004, Event: "PAUSED"}))

	if !errors.Is(err, boom) {
		t.Fatalf("want the recording error, got %v", err)
	}
	if repo.templates["t1"].Status != template.TemplateStatusPaused {
		t.Fatal("the template itself is still updated")
	}
}

func TestAnEventWithoutTemplateIDIsNotRecorded(t *testing.T) {
	recorder := &fakeQualityRecorder{}
	uc := NewHandleTemplateWebhook(newMockTemplateRepo(), recorder)

	if err := uc.Execute(signalPayload(template.FieldMessageTemplateQualityUpdate, 1, template.TemplateWebhookValue{NewQualityScore: "RED"})); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(recorder.changes) != 0 {
		t.Fatalf("nothing identifies the template, got %+v", recorder.changes)
	}
}

type recordingHealthSignals struct{ changed []string }

func (r *recordingHealthSignals) TemplateChanged(externalID string) {
	r.changed = append(r.changed, externalID)
}

func TestQualityStatusAndCategoryChangesAreSignalledAfterTheyAreRecorded(t *testing.T) {
	signals := &recordingHealthSignals{}
	uc := NewHandleTemplateWebhook(newMockTemplateRepo(), &fakeQualityRecorder{})
	uc.(HealthSignalling).SetHealthSignals(signals)

	for _, field := range []string{template.FieldMessageTemplateQualityUpdate, template.FieldMessageTemplateStatusUpdate, template.FieldTemplateCategoryUpdate, template.FieldMessageTemplateComponentsUpdate} {
		_ = uc.Execute(signalPayload(field, 1791990878, template.TemplateWebhookValue{
			MessageTemplateID: 42, Event: "PAUSED", NewQualityScore: "YELLOW", NewCategory: "MARKETING",
		}))
	}

	if len(signals.changed) != 3 || signals.changed[0] != "42" {
		t.Fatalf("signalled %v, want the three health changes and not the components update", signals.changed)
	}
}

func TestAChangeThatCouldNotBeRecordedIsNotSignalled(t *testing.T) {
	signals := &recordingHealthSignals{}
	uc := NewHandleTemplateWebhook(newMockTemplateRepo(), &fakeQualityRecorder{err: errors.New("db down")})
	uc.(HealthSignalling).SetHealthSignals(signals)

	_ = uc.Execute(signalPayload(template.FieldMessageTemplateQualityUpdate, 1791990878, template.TemplateWebhookValue{MessageTemplateID: 42, NewQualityScore: "RED"}))

	if len(signals.changed) != 0 {
		t.Fatalf("signalled %v before the change was stored", signals.changed)
	}
}
