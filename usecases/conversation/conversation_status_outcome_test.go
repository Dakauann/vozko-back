package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type outcomeEntryRepo struct {
	wce.Repository
	status  string
	writes  []wce.ConversationStatusWrite
	failure error
}

func (r *outcomeEntryRepo) FindByID(string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{ID: "entry-1", ConversationStatus: r.status}, nil
}

func (r *outcomeEntryRepo) UpdateConversationStatus(_ string, write wce.ConversationStatusWrite) error {
	if r.failure != nil {
		return r.failure
	}
	r.writes = append(r.writes, write)
	r.status = write.Status
	return nil
}

type stubCaptureReader struct {
	capture *conversation.OutcomeCapture
	err     error
	calls   int
}

func (s *stubCaptureReader) OutcomeCaptureFor(context.Context, string) (*conversation.OutcomeCapture, error) {
	s.calls++
	return s.capture, s.err
}

type stubDepartmentResolver struct {
	departmentID string
	err          error
}

func (s stubDepartmentResolver) DepartmentIDForEntry(context.Context, string, string) (string, error) {
	return s.departmentID, s.err
}

func requiringCapture() *conversation.OutcomeCapture {
	enabled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	capture := &conversation.OutcomeCapture{
		Enabled:          true,
		EnabledAt:        &enabled,
		RequireOnFinish:  true,
		DurableThreshold: 30,
		Outcomes: []conversation.Outcome{
			{Code: "sale", Label: "Venda fechada", IsDurable: true},
			{Code: "no_answer", Label: "Sem resposta"},
		},
	}
	capture.Normalize()
	return capture
}

func outcomeServiceWith(
	t *testing.T,
	repo *outcomeEntryRepo,
	reader OutcomeCaptureReader,
) *ConversationStatusService {
	t.Helper()
	svc := NewConversationStatusService(repo)
	svc.SetWorkspaceResolver(func(string, string) string { return "ws1" })
	svc.SetClock(func() time.Time { return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC) })
	if reader != nil {
		svc.SetOutcomeCaptureReader(reader)
	}
	return svc
}

func TestFinishWithoutCaptureConfiguredIsUnchanged(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, nil)

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman,
		Reason: conversation.CloseReasonManual,
	})
	if err != nil {
		t.Fatalf("Finish() err = %v, want nil", err)
	}
	if len(repo.writes) != 1 || repo.writes[0].CloseOutcome != "" {
		t.Fatalf("Finish() wrote %+v, want one write with no outcome", repo.writes)
	}
}

func TestFinishRequiresAnOutcomeWhenCaptureDemandsOne(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman,
		Reason: conversation.CloseReasonManual,
	})
	if !errors.Is(err, conversation.ErrOutcomeRequired) {
		t.Fatalf("Finish() err = %v, want %v", err, conversation.ErrOutcomeRequired)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("Finish() wrote %+v despite the gate, want no write", repo.writes)
	}
	if repo.status != string(conversation.ConversationStatusOngoing) {
		t.Fatalf("Finish() left status %q, want the conversation still open", repo.status)
	}
}

func TestFinishStoresAValidatedOutcome(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source:      conversation.CloseSourceHuman,
		Reason:      conversation.CloseReasonManual,
		OutcomeCode: "SALE",
	})
	if err != nil {
		t.Fatalf("Finish() err = %v, want nil", err)
	}
	if len(repo.writes) != 1 || repo.writes[0].CloseOutcome != "sale" {
		t.Fatalf("Finish() wrote %+v, want the normalized outcome", repo.writes)
	}
	if repo.writes[0].ClosedAt == nil {
		t.Fatalf("Finish() wrote no closed_at; the quality denominator needs it")
	}
}

func TestFinishRejectsAnUnknownOutcome(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source:      conversation.CloseSourceHuman,
		Reason:      conversation.CloseReasonManual,
		OutcomeCode: "invented",
	})
	if !errors.Is(err, conversation.ErrOutcomeUnknown) {
		t.Fatalf("Finish() err = %v, want %v", err, conversation.ErrOutcomeUnknown)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("Finish() wrote %+v despite an unknown code", repo.writes)
	}
}

func TestFinishFailsClosedWhenThePolicyCannotBeRead(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	readErr := errors.New("database is down")
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{err: readErr})

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman,
		Reason: conversation.CloseReasonManual,
	})
	if !errors.Is(err, readErr) {
		t.Fatalf("Finish() err = %v, want the policy read error", err)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("Finish() closed the conversation without reading the policy: %+v", repo.writes)
	}
}

func TestFinishFailsClosedWhenTheWorkspaceCannotBeResolved(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := NewConversationStatusService(repo)
	svc.SetOutcomeCaptureReader(&stubCaptureReader{capture: requiringCapture()})

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman,
		Reason: conversation.CloseReasonManual,
	})
	if !errors.Is(err, ErrOutcomeWorkspaceUnknown) {
		t.Fatalf("Finish() err = %v, want %v", err, ErrOutcomeWorkspaceUnknown)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("Finish() wrote %+v with no resolvable workspace", repo.writes)
	}
}

func TestSetConversationStatusFinishedGoesThroughTheGate(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	reader := &stubCaptureReader{capture: requiringCapture()}
	svc := outcomeServiceWith(t, repo, reader)

	err := svc.SetConversationStatus("entry-1", string(shared.EntryTypeWhatsApp), conversation.ConversationStatusFinished)
	if !errors.Is(err, conversation.ErrOutcomeRequired) {
		t.Fatalf("SetConversationStatus(finished) err = %v, want %v", err, conversation.ErrOutcomeRequired)
	}
	if reader.calls == 0 {
		t.Fatalf("SetConversationStatus(finished) bypassed the outcome policy")
	}
}

func TestSetConversationStatusOngoingSkipsTheGate(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusNew)}
	reader := &stubCaptureReader{capture: requiringCapture()}
	svc := outcomeServiceWith(t, repo, reader)

	if err := svc.SetConversationStatus("entry-1", string(shared.EntryTypeWhatsApp), conversation.ConversationStatusOngoing); err != nil {
		t.Fatalf("SetConversationStatus(ongoing) err = %v, want nil", err)
	}
	if reader.calls != 0 {
		t.Fatalf("SetConversationStatus(ongoing) read the outcome policy %d times, want 0", reader.calls)
	}
}

func TestSystemAndAICloseStampReservedOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		source conversation.CloseSource
		reason conversation.CloseReason
		want   string
	}{
		{"auto close", conversation.CloseSourceSystem, conversation.CloseReasonCustomerIdle, conversation.OutcomeSystemAutoClose},
		{"max age", conversation.CloseSourceSystem, conversation.CloseReasonMaxAge, conversation.OutcomeSystemAutoClose},
		{"workflow", conversation.CloseSourceSystem, conversation.CloseReasonWorkflow, conversation.OutcomeWorkflowUnspecified},
		{"ai", conversation.CloseSourceAI, conversation.CloseReasonAIResolved, conversation.OutcomeAIUnspecified},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
			svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})

			err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
				Source: tc.source,
				Reason: tc.reason,
			})
			if err != nil {
				t.Fatalf("Finish() err = %v, want nil", err)
			}
			if len(repo.writes) != 1 || repo.writes[0].CloseOutcome != tc.want {
				t.Fatalf("Finish() wrote %+v, want outcome %q", repo.writes, tc.want)
			}
		})
	}
}

func TestDepartmentScopedCaptureNeedsAResolver(t *testing.T) {
	capture := requiringCapture()
	capture.DepartmentIDs = []string{"dept1"}

	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: capture})

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman,
		Reason: conversation.CloseReasonManual,
	})
	if !errors.Is(err, conversation.ErrOutcomeRequired) {
		t.Fatalf("Finish() with no department resolver err = %v, want %v", err, conversation.ErrOutcomeRequired)
	}

	svc.SetEntryDepartmentResolver(stubDepartmentResolver{departmentID: "dept2"})
	if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceHuman,
		Reason: conversation.CloseReasonManual,
	}); err != nil {
		t.Fatalf("Finish() outside the scoped departments err = %v, want nil", err)
	}
}

func TestDepartmentResolverFailureFailsClosed(t *testing.T) {
	capture := requiringCapture()
	capture.DepartmentIDs = []string{"dept1"}

	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: capture})
	resolveErr := errors.New("department lookup failed")
	svc.SetEntryDepartmentResolver(stubDepartmentResolver{err: resolveErr})

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source:      conversation.CloseSourceHuman,
		Reason:      conversation.CloseReasonManual,
		OutcomeCode: "sale",
	})
	if !errors.Is(err, resolveErr) {
		t.Fatalf("Finish() err = %v, want the department resolve error", err)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("Finish() wrote %+v without resolving the department", repo.writes)
	}
}
