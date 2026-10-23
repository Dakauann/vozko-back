package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/balance"
	"vozko/domain/campaign/launchcheck"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/metaerror"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace_phone_access"
	template_usecase "vozko/usecases/whatsapp/template"
)

type ExitVerdicts interface {
	ExitConfigured(ctx context.Context, workspaceID, templateID, campaignID string) (bool, error)
}

type LaunchCheckDeps struct {
	Campaigns interface {
		FindByID(id string) (*wc.Campaign, error)
	}
	Counts interface {
		CountByStatus(campaignID string) (*wce.StatusCounts, error)
	}
	Failures interface {
		FailureReasons(scope wce.ReportScope) ([]wce.FailureReasonCount, error)
	}
	Templates interface {
		FindByID(id string) (*template.Template, error)
	}
	Quality quality.Timeline
	Phones  interface {
		FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
	}
	PhoneAccess    workspace_phone_access.Repository
	TemplateGrants TemplateGrants
	Exits          ExitVerdicts
	Costs          template.TemplateCostReader
	Balances       balance.BalanceReader
	Caps           balance.MonthlySendCapUsageReader
	Forms          template_usecase.FlowPublications
	Now            func() time.Time
}

type LaunchCheck struct {
	d LaunchCheckDeps
}

func NewLaunchCheck(deps LaunchCheckDeps) *LaunchCheck {
	return &LaunchCheck{d: deps}
}

const (
	unreadAccess   = "access"
	unreadTemplate = "template"
	unreadQuality  = "template_quality"
	unreadPhone    = "phone"
	unreadExit     = "exit"
	unreadLastRun  = "last_run"
	unreadBalance  = "balance"
	unreadCap      = "monthly_cap"
	unreadForm     = "form"
)

type launchFacts struct {
	snapshot launchcheck.Snapshot
	template *template.Template
	phone    *businessphone.WhatsAppBusinessPhoneNumber
	quote    *template.SendCost
}

func (f *launchFacts) unread(what string) {
	f.snapshot.Unread = append(f.snapshot.Unread, what)
}

func (c *LaunchCheck) Run(ctx context.Context, campaignID string) (launchcheck.Report, error) {
	camp, err := c.d.Campaigns.FindByID(campaignID)
	if err != nil {
		return launchcheck.Report{}, err
	}
	counts, err := c.d.Counts.CountByStatus(campaignID)
	if err != nil {
		return launchcheck.Report{}, fmt.Errorf("count the campaign's contacts: %w", err)
	}
	facts := &launchFacts{snapshot: launchcheck.Snapshot{Pending: int(counts.Pending), Fresh: counts.Total == 0}}
	c.readHealth(ctx, camp, facts)
	c.readLastRun(camp, counts, facts)
	return launchcheck.Check(facts.snapshot), nil
}

func (c *LaunchCheck) Preview(ctx context.Context, camp *wc.Campaign, pending int) (launchcheck.Report, *template.SendCost) {
	facts := &launchFacts{snapshot: launchcheck.Snapshot{Pending: pending, Fresh: true}}
	c.readHealth(ctx, camp, facts)
	return launchcheck.Check(facts.snapshot), facts.quote
}

func (c *LaunchCheck) readHealth(ctx context.Context, camp *wc.Campaign, facts *launchFacts) {
	c.readPhone(camp, facts)
	if !camp.IsOrganic() {
		c.readTemplate(ctx, camp, facts)
		c.readExit(ctx, camp, facts)
		c.readBalance(camp, facts)
	}
	c.readAccess(camp, facts)
	c.readCap(camp, facts)
}

func (c *LaunchCheck) readPhone(camp *wc.Campaign, facts *launchFacts) {
	phone, err := c.d.Phones.FindByID(camp.BusinessPhoneID)
	if err != nil || phone == nil {
		facts.unread(unreadPhone)
		return
	}
	facts.phone = phone
	facts.snapshot.Phone = &launchcheck.PhoneFacts{Status: phone.Status}
}

func (c *LaunchCheck) readTemplate(ctx context.Context, camp *wc.Campaign, facts *launchFacts) {
	tmpl, err := c.d.Templates.FindByID(camp.TemplateID)
	if err != nil || tmpl == nil {
		facts.unread(unreadTemplate)
		return
	}
	facts.template = tmpl
	latest, err := c.d.Quality.LatestBySignal(quality.SubjectTemplate, tmpl.ExternalID)
	if err != nil {
		facts.unread(unreadQuality)
		return
	}
	reading := quality.ReadTemplate(latest)
	facts.snapshot.Template = &launchcheck.TemplateFacts{
		Ready:     tmpl.IsReadyToSend(),
		Quality:   reading.Quality,
		Flagged:   reading.Flagged,
		Category:  tmpl.Category,
		CreatedAs: template.TemplateCategory(camp.TemplateCategory),
	}
	c.readForm(ctx, camp, tmpl, facts)
}

func (c *LaunchCheck) readForm(ctx context.Context, camp *wc.Campaign, tmpl *template.Template, facts *launchFacts) {
	err := template_usecase.CheckFormPublished(ctx, c.d.Forms, camp.WorkspaceID, tmpl.WABAId, tmpl)
	switch {
	case errors.Is(err, template.ErrFlowNotPublished):
		facts.snapshot.Template.FormNotSendable = true
	case err != nil:
		facts.unread(unreadForm)
	}
}

func (c *LaunchCheck) readExit(ctx context.Context, camp *wc.Campaign, facts *launchFacts) {
	configured, err := c.d.Exits.ExitConfigured(ctx, camp.WorkspaceID, camp.TemplateID, camp.ID)
	if err != nil {
		slog.Warn("campaign.launch_check.unreadable", "workspace_id", camp.WorkspaceID, "campaign_id", camp.ID, "signal", unreadExit, "error", err.Error())
		facts.unread(unreadExit)
		return
	}
	facts.snapshot.ExitConfigured = &configured
}

func (c *LaunchCheck) readBalance(camp *wc.Campaign, facts *launchFacts) {
	if facts.template == nil || facts.snapshot.Pending <= 0 {
		return
	}
	quote, err := template_usecase.QuoteSend(c.d.Costs, c.d.Balances, camp.WorkspaceID, facts.template, int64(facts.snapshot.Pending))
	if err != nil {
		facts.unread(unreadBalance)
		return
	}
	facts.quote = &quote
	facts.snapshot.BalanceShort = !quote.Affordable
}

func (c *LaunchCheck) readAccess(camp *wc.Campaign, facts *launchFacts) {
	if facts.phone == nil {
		facts.unread(unreadAccess)
		return
	}
	mayUsePhone, err := hasTemporaryCampaignBusinessPhoneAccess(camp.WorkspaceID, camp.BusinessPhoneID, facts.phone, c.d.PhoneAccess)
	if err != nil {
		facts.unread(unreadAccess)
		return
	}
	mayUseTemplate := true
	if !camp.IsOrganic() {
		if c.d.TemplateGrants == nil {
			facts.unread(unreadAccess)
			return
		}
		mayUseTemplate, err = c.d.TemplateGrants.HasAccess(camp.WorkspaceID, camp.TemplateID)
		if err != nil {
			facts.unread(unreadAccess)
			return
		}
	}
	facts.snapshot.AccessAllowed = mayUsePhone && mayUseTemplate
}

func (c *LaunchCheck) readLastRun(camp *wc.Campaign, counts *wce.StatusCounts, facts *launchFacts) {
	scope, err := wce.CampaignScope(camp.ID)
	if err != nil {
		facts.unread(unreadLastRun)
		return
	}
	reasons, err := c.d.Failures.FailureReasons(scope)
	if err != nil {
		facts.unread(unreadLastRun)
		return
	}
	run := launchcheck.Run{Processed: int(counts.Dispatches()), Failures: map[int]int{}}
	for _, reason := range reasons {
		if !metaerror.FromMeta(reason.Code) {
			continue
		}
		run.Failures[reason.Code] += int(reason.Count)
		run.Processed += int(reason.Count)
	}
	facts.snapshot.LastRun = run
}

func (c *LaunchCheck) readCap(camp *wc.Campaign, facts *launchFacts) {
	usage, err := c.d.Caps.MonthlySendCapUsage(camp.WorkspaceID, c.d.Now())
	if err != nil {
		facts.unread(unreadCap)
		return
	}
	if usage == nil {
		return
	}
	room := int(max(0, usage.Remaining()))
	facts.snapshot.CapRoom = &room
}
