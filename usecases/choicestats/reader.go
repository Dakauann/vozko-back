package choicestats_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/choicestats"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
)

var ErrReaderUnwired = errors.New("choice stats: the reader needs its store, zones, workflows, campaigns and templates")

type ScopedWorkflows interface {
	Get(scope workflow.Scope, id string) (*workflow.Workflow, error)
}

type OfficialCampaigns interface {
	FindByID(campaignID string) (*wc.Campaign, error)
}

type UnofficialCampaigns interface {
	Owned(ctx context.Context, workspaceID string, scope uw.DepartmentScope, campaignID string) (*uwc.Campaign, error)
}

type WorkspaceTemplates interface {
	Get(workspaceID, templateID string) (*template.Template, error)
}

type TemplateFinder interface {
	FindByID(id string) (*template.Template, error)
}

type ReaderDeps struct {
	Store        choicestats.Store
	Zones        ZoneLocator
	Workflows    ScopedWorkflows
	Campaigns    OfficialCampaigns
	Unofficial   UnofficialCampaigns
	Templates    WorkspaceTemplates
	TemplateRepo TemplateFinder
	LinkBase     string
	Now          func() time.Time
}

type Reader struct {
	deps ReaderDeps
}

func NewReader(deps ReaderDeps) (*Reader, error) {
	if deps.Store == nil || deps.Zones == nil || deps.Workflows == nil || deps.Campaigns == nil ||
		deps.Templates == nil || deps.TemplateRepo == nil {
		return nil, ErrReaderUnwired
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Reader{deps: deps}, nil
}

func (r *Reader) Workflow(ctx context.Context, scope workflow.Scope, workflowID, from, to string, version *int) (*choicestats.WorkflowReport, error) {
	if version != nil && *version <= 0 {
		return nil, choicestats.ErrInvalidVersion
	}
	wf, err := r.deps.Workflows.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	if wf == nil {
		return nil, workflow.ErrWorkflowNotFound
	}
	loc, err := r.zone(ctx, wf.WorkspaceID)
	if err != nil {
		return nil, err
	}
	period, err := choicestats.NewPeriod(from, to, loc)
	if err != nil {
		return nil, err
	}
	rows, err := r.counts(ctx, choicestats.CountQuery{
		WorkspaceID: wf.WorkspaceID, Kind: choicestats.SourceWorkflow, SourceID: wf.ID, Version: version,
		From: period.FirstDate(), To: period.LastDate(),
	})
	if err != nil {
		return nil, err
	}
	return &choicestats.WorkflowReport{
		From: period.FirstDay(), To: period.LastDay(), Timezone: period.Timezone(), Version: version,
		Nodes: choicestats.SummarizeNodes(rows, workflow.ChoiceCanvas(&wf.Graph)),
	}, nil
}

func (r *Reader) WhatsAppCampaign(ctx context.Context, access wc.ReportAccess, campaignID, from, to string) (*choicestats.PromptReport, error) {
	campaign, err := r.deps.Campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	if !access.AllowsCampaign(campaign) {
		return nil, wc.ErrCampaignNotFound
	}
	tmpl, err := r.deps.TemplateRepo.FindByID(campaign.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("%w: the campaign template is unreadable: %w", choicestats.ErrUnavailable, err)
	}
	return r.prompt(ctx, campaign.WorkspaceID, campaign.CreatedAt, from, to, choicestats.CountQuery{
		Kind: choicestats.SourceWhatsAppCampaign, SourceID: campaign.ID,
	}, tmpl.ChoiceOptions(r.deps.LinkBase))
}

func (r *Reader) UnofficialCampaign(ctx context.Context, workspaceID string, scope uw.DepartmentScope, campaignID, from, to string) (*choicestats.PromptReport, error) {
	if r.deps.Unofficial == nil {
		return nil, uwc.ErrCampaignNotFound
	}
	campaign, err := r.deps.Unofficial.Owned(ctx, workspaceID, scope, campaignID)
	if err != nil {
		return nil, err
	}
	if campaign == nil {
		return nil, uwc.ErrCampaignNotFound
	}
	shown := campaign.Message.ShownOptionIDs()
	options := make([]choicestats.OfferedOption, 0, len(shown))
	for _, id := range shown {
		options = append(options, choicestats.OfferedOption{ID: id})
	}
	return r.prompt(ctx, campaign.WorkspaceID, campaign.CreatedAt, from, to, choicestats.CountQuery{
		Kind: choicestats.SourceUnofficialCampaign, SourceID: campaign.ID,
	}, options)
}

func (r *Reader) Template(ctx context.Context, workspaceID, templateID, from, to string) (*choicestats.PromptReport, error) {
	tmpl, err := r.deps.Templates.Get(workspaceID, templateID)
	if errors.Is(err, template.ErrTemplateAccessDenied) || err == nil && tmpl == nil {
		return nil, template.ErrTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.prompt(ctx, workspaceID, time.Time{}, from, to, choicestats.CountQuery{TemplateID: tmpl.ID}, tmpl.ChoiceOptions(r.deps.LinkBase))
}

func (r *Reader) prompt(ctx context.Context, workspaceID string, since time.Time, from, to string, q choicestats.CountQuery, options []choicestats.OfferedOption) (*choicestats.PromptReport, error) {
	loc, err := r.zone(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	period, err := choicestats.OptionalPeriod(from, to, since, r.deps.Now(), loc)
	if err != nil {
		return nil, err
	}
	q.WorkspaceID = workspaceID
	q.From, q.To = period.FirstDate(), period.LastDate()
	rows, err := r.counts(ctx, q)
	if err != nil {
		return nil, err
	}
	if options == nil {
		options = []choicestats.OfferedOption{}
	}
	report := &choicestats.PromptReport{Timezone: period.Timezone(), Prompt: choicestats.Summarize(rows, options)}
	if from != "" || to != "" {
		first, last := period.FirstDay(), period.LastDay()
		report.From, report.To = &first, &last
	}
	return report, nil
}

func (r *Reader) counts(ctx context.Context, q choicestats.CountQuery) ([]choicestats.CountRow, error) {
	rows, err := r.deps.Store.Counts(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", choicestats.ErrUnavailable, err)
	}
	return rows, nil
}

func (r *Reader) zone(ctx context.Context, workspaceID string) (*time.Location, error) {
	loc, err := r.deps.Zones.Location(ctx, workspaceID, "")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", choicestats.ErrUnavailable, err)
	}
	if loc == nil {
		return nil, fmt.Errorf("%w: %w", choicestats.ErrUnavailable, choicestats.ErrZoneUnknown)
	}
	return loc, nil
}
