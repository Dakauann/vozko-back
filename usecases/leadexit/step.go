package leadexit_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	"vozko/domain/choicestats"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	"vozko/domain/workflow"
	buttonaction_usecase "vozko/usecases/buttonaction"
	inbox_assignment_usecase "vozko/usecases/inbox_assignment"
	lead_usecase "vozko/usecases/lead"
	opportunity_usecase "vozko/usecases/opportunity"
	workflow_usecase "vozko/usecases/workflow"
)

type ButtonActions interface {
	ResolveTap(ctx context.Context, in buttonaction_usecase.TapInput) (buttonaction.Resolution, error)
	TemplateExit(ctx context.Context, key buttonaction_usecase.TemplateKey) (*buttonaction_usecase.TemplateExit, error)
}

type SentMessages interface {
	SentTemplate(ctx context.Context, messageID string) (*leadexit.SentTemplate, error)
}

type CampaignMessages interface {
	LastCampaignMessage(ctx context.Context, entryType, entryID string) (*leadexit.SentCampaignMessage, error)
}

type Consent interface {
	Record(ctx context.Context, workspaceID, leadID string, change lead.ConsentChange) (lead_usecase.ConsentOutcome, error)
	ClaimOptOutConfirmation(ctx context.Context, workspaceID, leadID, withdrawalEventID string) error
}

type HandOff interface {
	HandOffToHuman(workspaceID, entryID, entryType, toUserID string) error
	HandOffToRoulette(in ia.RouletteHandOff) (string, error)
}

type Profiles interface {
	Update(ctx context.Context, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error)
}

type Deals interface {
	ManageForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error)
}

type Leads interface {
	FindByID(workspaceID, id string) (*lead.Lead, error)
}

type Workflows interface {
	StartForConversation(in workflow_usecase.ConversationStart) (workflow_usecase.StartedRun, error)
}

type Delegations interface {
	DelegateAsSystem(ctx context.Context, in inbox_assignment_usecase.SystemDelegation) error
}

type CallLists interface {
	AddLead(ctx context.Context, add calllist.LeadAddition) (bool, error)
}

type Deps struct {
	Buttons     ButtonActions
	Sent        SentMessages
	Campaigns   CampaignMessages
	Runs        leadexit.RunStore
	Consent     Consent
	Leads       Leads
	HandOff     HandOff
	Profiles    Profiles
	Deals       Deals
	Workflows   Workflows
	Delegations Delegations
	CallLists   CallLists
	Events      ce.Logger
	Choices     choicestats.Recorder
	Now         func() time.Time
}

type Step struct {
	deps Deps
}

var _ leadexit.Step = (*Step)(nil)

func NewStep(deps Deps) (*Step, error) {
	if deps.Buttons == nil || deps.Sent == nil || deps.Campaigns == nil || deps.Runs == nil || deps.Consent == nil || deps.Leads == nil ||
		deps.HandOff == nil || deps.Profiles == nil || deps.Deals == nil || deps.Workflows == nil || deps.Delegations == nil || deps.CallLists == nil || deps.Events == nil || deps.Choices == nil {
		return nil, leadexit.ErrUnavailable
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Step{deps: deps}, nil
}

type tapContext struct {
	resolution *buttonaction.Resolution
	replied    *leadexit.SentTemplate
	mismatch   bool
}

func (s *Step) Run(ctx context.Context, in leadexit.Inbound, replier leadexit.Replier) (leadexit.Result, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.MessageID) == "" {
		return leadexit.Result{}, leadexit.ErrInboundIncomplete
	}
	tc, err := s.resolveContext(ctx, in)
	if err != nil {
		return leadexit.Result{}, err
	}
	s.recordChoice(ctx, in, tc)
	result := leadexit.Result{Selection: selectionOf(in, tc)}
	if tc.mismatch || strings.TrimSpace(in.LeadID) == "" {
		return result, nil
	}
	candidates, ok, err := s.candidates(ctx, in, tc)
	if err != nil || !ok {
		return result, err
	}
	decision, ok := leadexit.Decide(candidates)
	if !ok {
		return result, nil
	}
	run, err := s.handle(ctx, in, tc, decision, replier)
	result.OptedOut = run.OptedOut()
	return result, err
}

func (s *Step) resolveContext(ctx context.Context, in leadexit.Inbound) (tapContext, error) {
	contextID := strings.TrimSpace(in.ContextMessageID)
	if contextID == "" {
		return tapContext{}, nil
	}
	sent, err := s.deps.Sent.SentTemplate(ctx, contextID)
	if err != nil {
		return tapContext{}, fmt.Errorf("resolve the replied message %s: %w", contextID, err)
	}
	if sent == nil {
		return tapContext{}, nil
	}
	if !sent.SentTo(in.WorkspaceID, in.SenderNumber) {
		if in.Tap != nil {
			log.Printf("[lead-exit] button_context_mismatch: inbound %s in workspace %s replied to %s, sent elsewhere", in.MessageID, in.WorkspaceID, contextID)
			return tapContext{mismatch: true}, nil
		}
		return tapContext{}, nil
	}
	tc := tapContext{replied: sent}
	if in.Tap == nil {
		return tc, nil
	}
	resolution, err := s.deps.Buttons.ResolveTap(ctx, buttonaction_usecase.TapInput{
		TemplateKey: templateKey(in.WorkspaceID, *sent), CampaignID: sent.CampaignID, Payload: in.Tap.Payload, Text: in.Tap.Text,
	})
	if err != nil {
		return tapContext{}, fmt.Errorf("resolve the tap on %s: %w", contextID, err)
	}
	tc.resolution = &resolution
	return tc, nil
}

func selectionOf(in leadexit.Inbound, tc tapContext) *workflow.OptionSelection {
	if in.Tap == nil {
		return nil
	}
	matched := leadexit.ButtonMatched(tc.resolution)
	index := 0
	if matched {
		index = tc.resolution.ButtonIndex
	}
	return workflow.TemplateButtonSelection(in.Tap.Text, index, matched)
}

func (s *Step) candidates(ctx context.Context, in leadexit.Inbound, tc tapContext) (leadexit.Candidates, bool, error) {
	c := leadexit.Candidates{Text: in.Text, Tap: tc.resolution}
	if !buttonaction.MayBeExitCommand(in.Text) && !resolvedAction(tc.resolution) {
		return c, false, nil
	}
	templates, err := s.templateWords(ctx, in.WorkspaceID, leadexit.TemplatesInOrder(tc.replied, in.LastTemplate))
	if err != nil {
		return c, false, err
	}
	c.Templates = templates
	if !buttonaction.MayBeExitCommand(in.Text) {
		return c, true, nil
	}
	campaign, err := s.deps.Campaigns.LastCampaignMessage(ctx, in.EntryType, in.EntryID)
	if err != nil {
		return c, false, fmt.Errorf("last campaign message of %s: %w", in.EntryID, err)
	}
	c.Campaign = campaign
	return c, true, nil
}

func resolvedAction(r *buttonaction.Resolution) bool {
	return r != nil && r.Action != nil && r.Reason == buttonaction.ReasonResolved
}

func (s *Step) templateWords(ctx context.Context, workspaceID string, sent []leadexit.SentTemplate) ([]leadexit.TemplateWords, error) {
	out := make([]leadexit.TemplateWords, 0, len(sent))
	for _, template := range sent {
		exit, err := s.deps.Buttons.TemplateExit(ctx, templateKey(workspaceID, template))
		if errors.Is(err, buttonaction.ErrTemplateNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("exit of template %s: %w", template.Template.Name, err)
		}
		out = append(out, leadexit.TemplateWords{Template: exit.Template, Words: exit.Words, Confirmation: exit.Confirmation})
	}
	return out, nil
}

func templateKey(workspaceID string, sent leadexit.SentTemplate) buttonaction_usecase.TemplateKey {
	return buttonaction_usecase.TemplateKey{
		WorkspaceID: workspaceID, TemplateID: sent.TemplateID, TemplateName: sent.Template.Name, Language: sent.Template.Language,
	}
}

func (s *Step) handle(ctx context.Context, in leadexit.Inbound, tc tapContext, d leadexit.Decision, replier leadexit.Replier) (leadexit.Run, error) {
	run := leadexit.Run{
		WorkspaceID: in.WorkspaceID, InboundMessageID: in.MessageID, EntryID: in.EntryID, EntryType: in.EntryType, LeadID: in.LeadID,
		Trigger: d.Trigger, Kind: d.Kind,
	}
	if tc.replied != nil {
		run.TemplateMessageID = tc.replied.MessageID
	}
	claim, err := s.deps.Runs.Claim(ctx, run)
	if err != nil {
		return leadexit.Run{}, fmt.Errorf("claim the exit run of %s: %w", in.MessageID, err)
	}
	if !claim.Claimed {
		if !claim.Previous.Outcome.Final() {
			return claim.Previous, leadexit.ErrRunInProgress
		}
		return claim.Previous, nil
	}
	run = s.perform(ctx, in, d, run, replier)
	run.UpdatedAt = s.deps.Now().UTC()
	finishErr := s.deps.Runs.Finish(context.WithoutCancel(ctx), run)
	s.writeLine(in, d, run)
	if run.Outcome == leadexit.OutcomeFailed {
		return run, fmt.Errorf("lead exit %s on %s failed: %s", run.Kind, in.MessageID, run.Reason)
	}
	if finishErr != nil {
		return run, fmt.Errorf("finish the exit run of %s: %w", in.MessageID, finishErr)
	}
	return run, nil
}

func (s *Step) perform(ctx context.Context, in leadexit.Inbound, d leadexit.Decision, run leadexit.Run, replier leadexit.Replier) leadexit.Run {
	bounded, cancel := context.WithTimeout(ctx, leadexit.RunTimeout)
	defer cancel()
	run = s.execute(bounded, in, d, run)
	return s.confirm(bounded, in, d, run, replier)
}

func (s *Step) writeLine(in leadexit.Inbound, d leadexit.Decision, run leadexit.Run) {
	event := ce.New(in.WorkspaceID, in.EntryID, in.EntryType, ce.EventLeadExitAction).
		WithChannel(in.Channel).
		WithCorrelation(in.MessageID).
		WithDetails(run.Details(d)).
		Build()
	s.deps.Events.Log(event)
}

func (s *Step) recordChoice(ctx context.Context, in leadexit.Inbound, tc tapContext) {
	if tc.replied == nil {
		return
	}
	option := choicestats.OptionFreeText
	if in.Tap != nil {
		if !leadexit.ButtonMatched(tc.resolution) {
			return
		}
		option = choicestats.TemplateButtonOptionID(tc.resolution.ButtonIndex)
	}
	s.deps.Choices.RecordClick(ctx, choicestats.Click{
		WorkspaceID:       in.WorkspaceID,
		ProviderMessageID: tc.replied.MessageID,
		Option:            option,
		At:                s.deps.Now().UTC(),
	})
}
