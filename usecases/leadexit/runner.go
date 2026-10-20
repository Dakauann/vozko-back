package leadexit_usecase

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	"vozko/domain/campaign"
	"vozko/domain/consentevidence"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	inbox_assignment_usecase "vozko/usecases/inbox_assignment"
	lead_usecase "vozko/usecases/lead"
	opportunity_usecase "vozko/usecases/opportunity"
	workflow_usecase "vozko/usecases/workflow"
)

const reasonConfirmationFailed = "lead_exit_confirmation_failed"

type effect struct {
	changed        bool
	assignedUserID string
	consentEventID string
	afterward      error
}

func (s *Step) execute(ctx context.Context, in leadexit.Inbound, d leadexit.Decision, run leadexit.Run) leadexit.Run {
	done, err := s.runKind(ctx, in, d)
	switch {
	case err == nil && done.changed:
		run.Outcome = leadexit.OutcomeChanged
		if done.afterward != nil {
			run.Reason = leadexit.ReasonCode(done.afterward)
			log.Printf("[lead-exit] %s on %s took effect, then failed: %v", d.Kind, in.MessageID, done.afterward)
		}
	case err == nil:
		run.Outcome = leadexit.OutcomeUnchanged
	case leadexit.IsRefusal(err):
		run.Outcome, run.Reason = leadexit.OutcomeRefused, leadexit.ReasonCode(err)
		log.Printf("[lead-exit] %s on %s refused: %v", d.Kind, in.MessageID, err)
	default:
		run.Outcome, run.Reason = leadexit.OutcomeFailed, leadexit.ReasonCode(err)
		log.Printf("[lead-exit] %s on %s failed: %v", d.Kind, in.MessageID, err)
	}
	run.AssignedUserID, run.ConsentEventID = done.assignedUserID, done.consentEventID
	return run
}

func (s *Step) runKind(ctx context.Context, in leadexit.Inbound, d leadexit.Decision) (effect, error) {
	params := buttonaction.Params{}
	if d.Action != nil {
		params = d.Action.Params
	}
	switch d.Kind {
	case buttonaction.KindOptOut:
		return s.recordConsent(ctx, in, d, lead.ConsentWithdraw, "")
	case buttonaction.KindConsentGrant:
		return s.recordConsent(ctx, in, d, lead.ConsentGrant, params.Purpose)
	case buttonaction.KindHandOff:
		return s.handOff(in, params)
	case buttonaction.KindDelegate:
		return s.delegate(ctx, in, params)
	case buttonaction.KindClassify:
		return s.classify(ctx, in, params)
	case buttonaction.KindCreateDeal:
		return s.createDeal(in, params)
	case buttonaction.KindStartWorkflow:
		return s.startWorkflow(in, d, params)
	case buttonaction.KindCallList:
		return s.addToCallList(ctx, in, params)
	}
	return effect{}, buttonaction.ErrKindUnknown
}

func (s *Step) recordConsent(ctx context.Context, in leadexit.Inbound, d leadexit.Decision, kind lead.ConsentKind, purpose string) (effect, error) {
	out, err := s.deps.Consent.Record(ctx, in.WorkspaceID, in.LeadID, consentChange(in, d, kind, purpose))
	if err != nil {
		return effect{}, err
	}
	return effect{changed: out.Changed, consentEventID: out.EventID}, nil
}

func consentChange(in leadexit.Inbound, d leadexit.Decision, kind lead.ConsentKind, purpose string) lead.ConsentChange {
	change := lead.ConsentChange{
		Kind:     kind,
		Source:   lead.ConsentKeyword,
		Actor:    lead.ConsentActor{Type: lead.ConsentActorLead, ID: in.LeadID},
		Purpose:  purpose,
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: in.EvidenceRef(), Excerpt: consentevidence.Excerpt(in.Text)},
	}
	if d.Trigger == leadexit.TriggerButton {
		change.Source = lead.ConsentButtonReply
		change.Evidence.Type = lead.EvidenceButtonReply
	}
	return change
}

func (s *Step) handOff(in leadexit.Inbound, p buttonaction.Params) (effect, error) {
	if p.UserID != "" {
		if err := s.deps.HandOff.HandOffToHuman(in.WorkspaceID, in.EntryID, in.EntryType, p.UserID); err != nil {
			return effect{}, err
		}
		return effect{changed: true, assignedUserID: p.UserID}, nil
	}
	assigned, err := s.deps.HandOff.HandOffToRoulette(ia.RouletteHandOff{
		WorkspaceID: in.WorkspaceID, EntryID: in.EntryID, EntryType: in.EntryType, DepartmentID: p.DepartmentID, ByActorID: actor.SystemID,
	})
	if assigned != "" {
		return effect{changed: true, assignedUserID: assigned, afterward: err}, nil
	}
	if err != nil {
		return effect{}, err
	}
	return effect{changed: true}, nil
}

func (s *Step) delegate(ctx context.Context, in leadexit.Inbound, p buttonaction.Params) (effect, error) {
	err := s.deps.Delegations.DelegateAsSystem(ctx, inbox_assignment_usecase.SystemDelegation{
		WorkspaceID: in.WorkspaceID, EntryID: in.EntryID, EntryType: shared.EntryType(in.EntryType), Automation: p.Automation(),
	})
	if err != nil {
		return effect{}, err
	}
	return effect{changed: true}, nil
}

func (s *Step) classify(ctx context.Context, in leadexit.Inbound, p buttonaction.Params) (effect, error) {
	out, err := s.deps.Profiles.Update(ctx, lead_usecase.ProfileUpdate{
		WorkspaceID: in.WorkspaceID, LeadID: in.LeadID, Actor: actor.SystemID, Source: lead.ProfileFromConversation,
		Profile: lead.Profile{CustomFields: map[string]string{p.FieldKey: p.Option}},
	})
	if err != nil {
		return effect{}, err
	}
	return effect{changed: len(out.Changed) > 0}, nil
}

func (s *Step) createDeal(in leadexit.Inbound, p buttonaction.Params) (effect, error) {
	out, err := s.deps.Deals.ManageForEntry(in.WorkspaceID, opportunity_usecase.EntryCommand{
		EntryID: in.EntryID, EntryType: in.EntryType, LeadID: in.LeadID, PipelineID: p.PipelineID, StageID: p.StageID,
		Actor: actor.SystemID, Action: opportunity_usecase.EntryCreate,
	})
	if err != nil {
		return effect{}, err
	}
	return effect{changed: out != nil && out.Created}, nil
}

func (s *Step) startWorkflow(in leadexit.Inbound, d leadexit.Decision, p buttonaction.Params) (effect, error) {
	started, err := s.deps.Workflows.StartForConversation(workflow_usecase.ConversationStart{
		WorkspaceID: in.WorkspaceID, WorkflowID: p.WorkflowID, EntryID: in.EntryID, EntryType: in.EntryType,
		StartedBy: workflow.StartedByExit(in.MessageID), Data: tapRunData(in, d),
	})
	if err != nil {
		return effect{}, err
	}
	return effect{changed: !started.AlreadyRunning}, nil
}

func tapRunData(in leadexit.Inbound, d leadexit.Decision) map[string]interface{} {
	data := map[string]interface{}{
		"message":           in.Text,
		workflow.VarChannel: in.EntryType,
		"workspace_id":      in.WorkspaceID,
	}
	if in.Tap != nil && d.Action != nil {
		workflow.ApplySelection(data, workflow.TemplateButtonSelection(in.Tap.Text, d.Action.ButtonIndex, true))
	}
	workflow.ApplyContactNumber(data, in.SenderNumber)
	workflow.ApplyTriggerMessage(data, in.TriggerMessageID)
	return data
}

func (s *Step) addToCallList(ctx context.Context, in leadexit.Inbound, p buttonaction.Params) (effect, error) {
	added, err := s.deps.CallLists.AddLead(ctx, calllist.LeadAddition{WorkspaceID: in.WorkspaceID, ListID: p.CallListID, LeadID: in.LeadID})
	if err != nil {
		return effect{}, err
	}
	return effect{changed: added}, nil
}

func (s *Step) confirm(ctx context.Context, in leadexit.Inbound, d leadexit.Decision, run leadexit.Run, replier leadexit.Replier) leadexit.Run {
	run.Confirmation = leadexit.ConfirmationNone
	if run.Outcome != leadexit.OutcomeChanged || strings.TrimSpace(d.Confirmation) == "" {
		return run
	}
	if !leadexit.WindowOpen(in.ReceivedAt, s.deps.Now()) {
		run.Confirmation = leadexit.ConfirmationOutsideWindow
		return run
	}
	if replier == nil {
		run.Confirmation, run.ConfirmationReason = leadexit.ConfirmationNotSent, leadexit.ErrorCode(leadexit.ErrUnavailable)
		return run
	}
	if d.Kind == buttonaction.KindOptOut {
		if err := s.deps.Consent.ClaimOptOutConfirmation(ctx, in.WorkspaceID, in.LeadID, run.ConsentEventID); err != nil {
			return claimRefused(run, err)
		}
	}
	if !buttonaction.IsExit(d.Kind) {
		if err := s.receives(in); err != nil {
			run.Confirmation, run.ConfirmationReason = leadexit.ConfirmationNotSent, leadexit.ReasonCode(err)
			return run
		}
	}
	if err := replier.Reply(ctx, d.Confirmation); err != nil {
		log.Printf("[lead-exit] confirmation of %s on %s not sent: %v", d.Kind, in.MessageID, err)
		run.Confirmation, run.ConfirmationReason = leadexit.ConfirmationNotSent, reasonConfirmationFailed
		return run
	}
	run.Confirmation = leadexit.ConfirmationSent
	return run
}

func (s *Step) receives(in leadexit.Inbound) error {
	contact, err := s.deps.Leads.FindByID(in.WorkspaceID, in.LeadID)
	if err != nil {
		log.Printf("[lead-exit] the lead of %s could not be checked before its confirmation: %v", in.MessageID, err)
		return err
	}
	if contact == nil {
		return lead.ErrLeadNotFound
	}
	return campaign.ContactRefusal(contact)
}

func claimRefused(run leadexit.Run, err error) leadexit.Run {
	if errors.Is(err, lead.ErrLeadOptedOut) {
		run.Confirmation, run.ConfirmationReason = leadexit.ConfirmationAlreadySent, leadexit.ReasonCode(err)
		return run
	}
	log.Printf("[lead-exit] the opt-out confirmation of %s could not be claimed: %v", run.InboundMessageID, err)
	run.Confirmation, run.ConfirmationReason = leadexit.ConfirmationNotSent, leadexit.ReasonCode(err)
	return run
}
