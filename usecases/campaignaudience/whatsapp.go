package campaignaudience_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/usecases/campaignautomation"
	"vozko/usecases/campaigncreate"
	"vozko/usecases/campaignguard"
)

type Screener interface {
	Screen(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (campaignguard.Screening, error)
	ScreenWithRunning(ctx context.Context, workspaceID string, leadIDs []string, senderID string) (campaignguard.Screening, error)
}

type WhatsAppDeps struct {
	Screener    Screener
	Subjects    campaigncreate.SubjectSource
	Definitions Definitions
	Templates   TemplateReader
	Suggestions Suggestions
	Instances   uwc.CampaignInstanceUseCase
	Scopes      uw.DepartmentScopeSource
	Automation  campaignautomation.Rules
}

type officialChannel struct{ deps WhatsAppDeps }

type unofficialChannel struct{ deps WhatsAppDeps }

func (d WhatsAppDeps) shared() error {
	if d.Screener == nil || !d.Subjects.Ready() || d.Definitions == nil || d.Automation == nil {
		return fmt.Errorf("%w: whatsapp screening, lead subjects, lead fields and the automation rules are required", campaignaudience.ErrUnavailable)
	}
	return nil
}

func NewOfficialChannel(deps WhatsAppDeps) (Channel, error) {
	if err := deps.shared(); err != nil {
		return nil, err
	}
	if deps.Templates == nil || deps.Suggestions == nil {
		return nil, fmt.Errorf("%w: templates and their suggestions are required", campaignaudience.ErrUnavailable)
	}
	return &officialChannel{deps: deps}, nil
}

func NewUnofficialChannel(deps WhatsAppDeps) (Channel, error) {
	if err := deps.shared(); err != nil {
		return nil, err
	}
	if deps.Instances == nil || deps.Scopes == nil {
		return nil, fmt.Errorf("%w: numbers and their department scopes are required", campaignaudience.ErrUnavailable)
	}
	return &unofficialChannel{deps: deps}, nil
}

func (c *officialChannel) Name() campaignaudience.Channel { return campaignaudience.ChannelWhatsApp }

func (c *unofficialChannel) Name() campaignaudience.Channel {
	return campaignaudience.ChannelUnofficial
}

func (d WhatsAppDeps) definitions(workspaceID string) ([]*customfield.Definition, error) {
	defs, err := d.Definitions.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("lead fields of workspace %s: %w", workspaceID, err)
	}
	return defs, nil
}

func (d WhatsAppDeps) metadataRule(workspaceID string, automation campaign.Automation) (campaignaudience.MetadataRule, error) {
	rule, err := d.Automation.Rule(workspaceID, automation)
	if err != nil {
		return nil, err
	}
	return rule.Missing, nil
}

type preparation struct {
	senderID string
	slots    []campaignaudience.Slot
	awaits   campaignaudience.Blocker
}

func (d WhatsAppDeps) prepared(a Actor, p preparation, automation campaign.Automation) (Prepared, error) {
	defs, err := d.definitions(a.WorkspaceID)
	if err != nil {
		return Prepared{}, err
	}
	metadata, err := d.metadataRule(a.WorkspaceID, automation)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Slots: usableSuggestions(p.slots, defs), Awaits: p.awaits, Metadata: metadata, Open: func(ctx context.Context, bound []campaignaudience.Bound, source campaignaudience.Source) (Judge, error) {
		return d.judge(a, p.senderID, bound, source, defs)
	}}, nil
}

func (c *officialChannel) Prepare(ctx context.Context, a Actor, params Params) (Prepared, error) {
	senderID := strings.TrimSpace(params.BusinessPhoneID)
	templateID := strings.TrimSpace(params.TemplateID)
	if templateID == "" {
		return c.deps.prepared(a, preparation{senderID: senderID, awaits: campaignaudience.BlockerTemplateRequired}, params.Automation)
	}
	tmpl, err := c.deps.Templates.Get(a.WorkspaceID, templateID)
	if err != nil {
		return Prepared{}, fmt.Errorf("%w: %w", wc.ErrCampaignTemplateNotFound, err)
	}
	if tmpl == nil {
		return Prepared{}, wc.ErrCampaignTemplateNotFound
	}
	if err := tmpl.EnsureSendable(); err != nil {
		return Prepared{}, err
	}
	slots, err := c.templateSlots(ctx, a.WorkspaceID, tmpl)
	if err != nil {
		return Prepared{}, err
	}
	return c.deps.prepared(a, preparation{senderID: senderID, slots: slots}, params.Automation)
}

func (c *officialChannel) templateSlots(ctx context.Context, workspaceID string, tmpl *template.Template) ([]campaignaudience.Slot, error) {
	stored, err := c.deps.Suggestions.Get(ctx, workspaceID, tmpl.ID)
	if err != nil {
		return nil, err
	}
	params := tmpl.ParameterSlots()
	slots := campaignaudience.SuggestedSlots(len(params), stored)
	for i, p := range params {
		slots[i].Name, slots[i].Header, slots[i].Example = p.Name, p.Header, p.Example
	}
	return slots, nil
}

func usableSuggestions(slots []campaignaudience.Slot, defs []*customfield.Definition) []campaignaudience.Slot {
	for i, s := range slots {
		if s.Suggested != "" && campaign.ValidateBindingSource(s.Suggested, defs) != nil {
			slots[i].Suggested = ""
		}
	}
	return slots
}

func (c *unofficialChannel) Prepare(ctx context.Context, a Actor, params Params) (Prepared, error) {
	instanceID := strings.TrimSpace(params.InstanceID)
	if instanceID != "" {
		scope, allowed := uw.ResolveScope(c.deps.Scopes, a.UserID, a.WorkspaceID, a.IsAdmin)
		if !allowed {
			return Prepared{}, uw.ErrInstanceOutsideDepartment
		}
		if _, err := c.deps.Instances.Usable(ctx, a.WorkspaceID, scope, instanceID); err != nil {
			return Prepared{}, err
		}
	}
	if params.Message == nil {
		return c.deps.prepared(a, preparation{senderID: instanceID, awaits: campaignaudience.BlockerMessageRequired}, params.Automation)
	}
	return c.deps.prepared(a, preparation{senderID: instanceID, slots: campaignaudience.SuggestedSlots(params.Message.ParameterCount(), nil)}, params.Automation)
}

func (d WhatsAppDeps) judge(a Actor, senderID string, bound []campaignaudience.Bound, source campaignaudience.Source, defs []*customfield.Definition) (Judge, error) {
	plan, err := campaign.PlanBindings(campaignaudience.Bindings(bound), len(bound), defs)
	if err != nil {
		return nil, err
	}
	return &whatsappJudge{deps: d, workspaceID: a.WorkspaceID, senderID: senderID, plan: plan, bound: bound, source: source}, nil
}

type whatsappJudge struct {
	deps        WhatsAppDeps
	workspaceID string
	senderID    string
	plan        campaign.BindingPlan
	bound       []campaignaudience.Bound
	source      campaignaudience.Source
}

func (j *whatsappJudge) Leads(ctx context.Context, ids []string) ([]campaignaudience.Candidate, error) {
	screening, err := j.deps.Screener.ScreenWithRunning(ctx, j.workspaceID, ids, j.senderID)
	if err != nil {
		return nil, err
	}
	subjects, err := j.deps.Subjects.Subjects(ctx, j.workspaceID, ids, j.plan)
	if err != nil {
		return nil, err
	}
	out := make([]campaignaudience.Candidate, 0, len(ids))
	for _, id := range ids {
		refusal := screening.Skipped[id]
		c := campaignaudience.Candidate{LeadID: id, HasContact: refusal != campaign.SkipNoIdentity, Refusal: string(refusal), Values: j.plan.Values(subjects[id])}
		if l := subjects[id].Lead; l != nil {
			c.Contact, c.Name = l.Number, l.Name
		}
		out = append(out, c)
	}
	return out, nil
}

func (j *whatsappJudge) Rows(ctx context.Context, rows []RowCandidate) ([]campaignaudience.Candidate, error) {
	var existing []string
	for _, r := range rows {
		if r.LeadID != "" {
			existing = append(existing, r.LeadID)
		}
	}
	skipped := map[string]campaign.SkipReason{}
	var known map[string]campaign.BindingSubject
	if len(existing) > 0 {
		screening, err := j.deps.Screener.Screen(ctx, j.workspaceID, existing, j.senderID)
		if err != nil {
			return nil, err
		}
		skipped = screening.Skipped
		if j.fills(rows) {
			if known, err = j.deps.Subjects.Subjects(ctx, j.workspaceID, existing, j.plan); err != nil {
				return nil, err
			}
		}
	}
	out := make([]campaignaudience.Candidate, 0, len(rows))
	for _, r := range rows {
		values := j.filled(r, known)
		out = append(out, campaignaudience.Candidate{
			Line: r.Row.Line, LeadID: r.LeadID, Contact: r.Row.Contact, Name: r.Row.Name, HasContact: true,
			Refusal: string(skipped[r.LeadID]), Values: values, Metadata: r.Metadata,
		})
	}
	return out, nil
}

func (j *whatsappJudge) fillable(i int) bool {
	return i < len(j.bound) && !campaignaudience.Unbound(j.bound[i])
}

func (j *whatsappJudge) fills(rows []RowCandidate) bool {
	for _, r := range rows {
		for i, v := range r.Values {
			if strings.TrimSpace(v) == "" && j.fillable(i) {
				return true
			}
		}
	}
	return false
}

func (j *whatsappJudge) filled(r RowCandidate, known map[string]campaign.BindingSubject) []string {
	values := make([]string, len(j.bound))
	copy(values, r.Values)
	var resolved []string
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
		if values[i] != "" || !j.fillable(i) {
			continue
		}
		if resolved == nil {
			resolved = j.plan.Values(subjectOfRow(r, known))
		}
		values[i] = resolved[i]
	}
	return values
}

func subjectOfRow(r RowCandidate, known map[string]campaign.BindingSubject) campaign.BindingSubject {
	subject, found := known[r.LeadID]
	if !found || subject.Lead == nil {
		return campaign.BindingSubject{Lead: &lead.Lead{Name: r.Row.Name}}
	}
	l := *subject.Lead
	if strings.TrimSpace(r.Row.Name) != "" {
		l.Name = r.Row.Name
	}
	subject.Lead = &l
	return subject
}
