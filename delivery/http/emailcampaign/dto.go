package emailcampaignhttp

import (
	"encoding/json"
	"strings"
	"time"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/crmfilter"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emaildesign"
	"vozko/domain/selection"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
	emailconsent_usecase "vozko/usecases/emailconsent"
)

type SelectionDTO struct {
	Mode       string            `json:"mode" enums:"ids,all_matching,first_n,everyone" example:"all_matching"`
	IDs        []string          `json:"ids,omitempty"`
	Filter     *crmfilter.Filter `json:"filter,omitempty"`
	Sort       []crmfilter.Sort  `json:"sort,omitempty"`
	Limit      int               `json:"limit,omitempty" example:"1000"`
	ExcludeIDs []string          `json:"exclude_ids,omitempty"`
	IncludeIDs []string          `json:"include_ids,omitempty"`
}

func (d *SelectionDTO) domain() *selection.Selection {
	if d == nil {
		return nil
	}
	return &selection.Selection{Mode: selection.Mode(strings.TrimSpace(d.Mode)), IDs: d.IDs, Filter: d.Filter, Sort: d.Sort, Limit: d.Limit, ExcludeIDs: d.ExcludeIDs, IncludeIDs: d.IncludeIDs}
}

func selectionDTO(s *selection.Selection) *SelectionDTO {
	if s == nil {
		return nil
	}
	return &SelectionDTO{Mode: string(s.Mode), IDs: s.IDs, Filter: s.Filter, Sort: s.Sort, Limit: s.Limit, ExcludeIDs: s.ExcludeIDs, IncludeIDs: s.IncludeIDs}
}

type MergePolicyDTO struct {
	Key     string `json:"key" example:"first_name"`
	Missing string `json:"missing,omitempty" enums:"text,exclude" example:"text"`
	Text    string `json:"text,omitempty" example:"cliente"`
}

func mergePolicies(in []MergePolicyDTO) []audience.Variable {
	out := make([]audience.Variable, 0, len(in))
	for _, p := range in {
		out = append(out, audience.Variable{Key: p.Key, Missing: audience.MissingPolicy(p.Missing), Text: p.Text})
	}
	return out
}

func mergePolicyDTOs(in []audience.Variable) []MergePolicyDTO {
	out := make([]MergePolicyDTO, 0, len(in))
	for _, p := range in {
		out = append(out, MergePolicyDTO{Key: p.Key, Missing: string(p.Missing), Text: p.Text})
	}
	return out
}

type MergeTagCountDTO struct {
	Key      string `json:"key" example:"first_name"`
	Missing  int    `json:"missing" example:"41"`
	Policy   string `json:"policy" enums:",text,exclude"`
	Text     string `json:"text,omitempty"`
	Fallback string `json:"fallback,omitempty" example:"cliente"`
}

type AudienceListDTO struct {
	ID       string `json:"id"`
	Rows     int    `json:"rows"`
	Existing int    `json:"existing"`
	New      int    `json:"new"`
}

type ABTestDTO struct {
	VariantBSubject    string `json:"variant_b_subject" example:"Só hoje: 30% off"`
	TestPercent        int    `json:"test_percent" example:"20"`
	WinnerMetric       string `json:"winner_metric" enums:"opens,clicks" example:"opens"`
	DecideAfterMinutes int    `json:"decide_after_minutes" example:"120"`
	Winner             string `json:"winner,omitempty" enums:"a,b"`
}

func (d *ABTestDTO) domain() *emailcampaign.ABTest {
	if d == nil {
		return nil
	}
	return &emailcampaign.ABTest{VariantBSubject: d.VariantBSubject, TestPercent: d.TestPercent, WinnerMetric: emailcampaign.WinnerMetric(d.WinnerMetric), DecideAfterMinutes: d.DecideAfterMinutes, Winner: emailcampaign.Variant(d.Winner)}
}

func abTestDTO(a *emailcampaign.ABTest) *ABTestDTO {
	if a == nil {
		return nil
	}
	return &ABTestDTO{VariantBSubject: a.VariantBSubject, TestPercent: a.TestPercent, WinnerMetric: string(a.WinnerMetric), DecideAfterMinutes: a.DecideAfterMinutes, Winner: string(a.Winner)}
}

type CampaignRequest struct {
	Name          string           `json:"name" example:"Black Friday"`
	SenderID      string           `json:"sender_id,omitempty"`
	Subject       string           `json:"subject" example:"Oferta para {{first_name|você}}"`
	Preheader     string           `json:"preheader,omitempty"`
	Document      json.RawMessage  `json:"document,omitempty" swaggertype:"object"`
	Selection     *SelectionDTO    `json:"selection,omitempty"`
	ListID        string           `json:"list_id,omitempty"`
	MergePolicies []MergePolicyDTO `json:"merge_policies,omitempty"`
	ABTest        *ABTestDTO       `json:"ab_test,omitempty"`
	UTM           *emaildesign.UTM `json:"utm,omitempty"`
}

func decodeDocument(raw json.RawMessage) (emaildesign.Document, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return emaildesign.Document{}, nil
	}
	return emaildesign.Decode(raw)
}

func (r CampaignRequest) draft() (emailcampaign.Draft, error) {
	doc, err := decodeDocument(r.Document)
	if err != nil {
		return emailcampaign.Draft{}, err
	}
	return emailcampaign.Draft{Name: r.Name, SenderID: r.SenderID, Subject: r.Subject, Preheader: r.Preheader, Document: doc, Selection: r.Selection.domain(), ListID: r.ListID, MergePolicies: mergePolicies(r.MergePolicies), ABTest: r.ABTest.domain(), UTM: r.UTM}, nil
}

type CampaignResponse struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Status        string               `json:"status" enums:"draft,ready,scheduled,running,paused,held,completed,stopped"`
	HoldReason    string               `json:"hold_reason,omitempty"`
	SenderID      string               `json:"sender_id,omitempty"`
	Subject       string               `json:"subject"`
	Preheader     string               `json:"preheader,omitempty"`
	Document      emaildesign.Document `json:"document"`
	Selection     *SelectionDTO        `json:"selection,omitempty"`
	ListID        string               `json:"list_id,omitempty"`
	MergePolicies []MergePolicyDTO     `json:"merge_policies"`
	ABTest        *ABTestDTO           `json:"ab_test,omitempty"`
	UTM           *emaildesign.UTM     `json:"utm,omitempty"`
	ScheduledAt   *time.Time           `json:"scheduled_at,omitempty"`
	StartedAt     *time.Time           `json:"started_at,omitempty"`
	CompletedAt   *time.Time           `json:"completed_at,omitempty"`
	Billing       BillingDTO           `json:"billing"`
	Hold          *HoldDTO             `json:"hold,omitempty"`
	TestBatch     *TestBatchDTO        `json:"test_batch,omitempty"`
	Actions       []string             `json:"actions" enums:"edit,delete,ready,schedule,unschedule,launch,start,resume,pause,stop"`
	TimeZone      string               `json:"time_zone" example:"America/Sao_Paulo"`
	CreatedBy     string               `json:"created_by,omitempty"`
	LaunchedBy    string               `json:"launched_by,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

type TestBatchDTO struct {
	Size      int        `json:"size" example:"200"`
	State     string     `json:"state" enums:"testing,passed,failed"`
	DrainedAt *time.Time `json:"drained_at"`
	DecidedAt *time.Time `json:"decided_at"`
}

func testBatchDTO(t *emailcampaign.TestBatch) *TestBatchDTO {
	if t == nil {
		return nil
	}
	return &TestBatchDTO{Size: t.Size, State: string(t.State), DrainedAt: t.DrainedAt, DecidedAt: t.DecidedAt}
}

type HoldDTO struct {
	Reason              string     `json:"reason" example:"daily_cap"`
	ResumeAt            *time.Time `json:"resume_at,omitempty"`
	AccountStatusReason string     `json:"account_status_reason,omitempty"`
}

func holdDTO(h *emailcampaign.Hold) *HoldDTO {
	if h == nil {
		return nil
	}
	return &HoldDTO{Reason: string(h.Reason), ResumeAt: h.ResumeAt, AccountStatusReason: h.AccountStatusReason}
}

type BillingDTO struct {
	BatchSize           int64      `json:"batch_size" example:"1000"`
	CurrentBatch        int        `json:"current_batch" example:"2"`
	ChargedUnits        int64      `json:"charged_units" example:"2000"`
	ChargedMicros       int64      `json:"charged_micros" example:"1166000"`
	SentUnits           int64      `json:"sent_units" example:"1400"`
	RefundedUnits       int64      `json:"refunded_units" example:"0"`
	RefundedMicros      int64      `json:"refunded_micros" example:"0"`
	FinalMicros         int64      `json:"final_micros" example:"1166000"`
	SettledAt           *time.Time `json:"settled_at"`
	RefundPendingMicros int64      `json:"refund_pending_micros" example:"349800"`
	ExchangeRate        *float64   `json:"exchange_rate" example:"5.5"`
	ChargedBRL          *float64   `json:"charged_brl" example:"6.41"`
	RefundedBRL         *float64   `json:"refunded_brl" example:"0"`
	FinalBRL            *float64   `json:"final_brl" example:"6.41"`
	RefundPendingBRL    *float64   `json:"refund_pending_brl" example:"1.92"`
}

func billingDTO(b emailcampaign.Billing, money *emailcampaign_usecase.BillingMoney) BillingDTO {
	out := BillingDTO{
		BatchSize: b.BatchSize, CurrentBatch: b.CurrentBatch, ChargedUnits: b.ChargedUnits, ChargedMicros: b.ChargedMicros, SentUnits: b.SentUnits,
		RefundedUnits: b.RefundedUnits, RefundedMicros: b.RefundedMicros, FinalMicros: b.FinalMicros, SettledAt: b.SettledAt, RefundPendingMicros: b.RefundPendingMicros,
	}
	if money != nil {
		rate, charged, refunded, final, pending := money.ExchangeRate, money.ChargedBRL, money.RefundedBRL, money.FinalBRL, money.RefundPendingBRL
		out.ExchangeRate, out.ChargedBRL, out.RefundedBRL, out.FinalBRL, out.RefundPendingBRL = &rate, &charged, &refunded, &final, &pending
	}
	return out
}

func commandNames(commands []emailcampaign.Command) []string {
	out := make([]string, 0, len(commands))
	for _, c := range commands {
		out = append(out, string(c))
	}
	return out
}

func campaignResponse(v emailcampaign_usecase.CampaignView) CampaignResponse {
	c := v.Campaign
	out := CampaignResponse{
		ID: c.ID, Name: c.Name, Status: string(c.Status), HoldReason: string(c.HoldReason), SenderID: c.SenderID,
		Subject: c.Subject, Preheader: c.Preheader, Document: c.Document, Selection: selectionDTO(c.Selection), ListID: c.ListID, MergePolicies: mergePolicyDTOs(c.MergePolicies),
		ABTest: abTestDTO(c.ABTest), UTM: c.UTM, ScheduledAt: c.ScheduledAt, StartedAt: c.StartedAt, CompletedAt: c.CompletedAt,
		CreatedBy: c.CreatedBy, LaunchedBy: c.LaunchedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		Billing: billingDTO(v.Billing, v.Money), Hold: holdDTO(v.Hold), Actions: commandNames(v.Actions), TimeZone: v.TimeZone,
		TestBatch: testBatchDTO(c.TestBatch),
	}
	if out.Document.Blocks == nil {
		out.Document.Blocks = []emaildesign.Block{}
	}
	return out
}

type CampaignSummaryResponse struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	HoldReason  string         `json:"hold_reason,omitempty"`
	Subject     string         `json:"subject"`
	ScheduledAt *time.Time     `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	ABTest      bool           `json:"ab_test"`
	CreatedAt   time.Time      `json:"created_at"`
	Metrics     ListMetricsDTO `json:"metrics"`
	Actions     []string       `json:"actions"`
}

type ListMetricsDTO struct {
	Sent         int64 `json:"sent"`
	Delivered    int64 `json:"delivered"`
	DeliveryRate int   `json:"delivery_rate"`
	OpenRate     int   `json:"open_rate"`
	ClickRate    int   `json:"click_rate"`
}

type CampaignListResponse struct {
	Items    []CampaignSummaryResponse `json:"items"`
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

func campaignList(items []emailcampaign_usecase.ListItem, total int64, page, size int) CampaignListResponse {
	out := CampaignListResponse{Items: make([]CampaignSummaryResponse, 0, len(items)), Total: total, Page: page, PageSize: size}
	for _, it := range items {
		c, m := it.Campaign, it.Metrics
		out.Items = append(out.Items, CampaignSummaryResponse{
			ID: c.ID, Name: c.Name, Status: string(c.Status), HoldReason: string(c.HoldReason), Subject: c.Subject,
			ScheduledAt: c.ScheduledAt, StartedAt: c.StartedAt, CompletedAt: c.CompletedAt, ABTest: c.ABTest != nil, CreatedAt: c.CreatedAt,
			Metrics: ListMetricsDTO{Sent: m.Sent, Delivered: m.Delivered, DeliveryRate: m.DeliveryRate, OpenRate: m.OpenRate, ClickRate: m.ClickRate},
			Actions: commandNames(it.Actions),
		})
	}
	return out
}

type QuoteDTO struct {
	Units       int64   `json:"units" example:"1200"`
	UnitMicros  int64   `json:"unit_micros" example:"583"`
	TotalMicros int64   `json:"total_micros" example:"699600"`
	UnitBRL     float64 `json:"unit_brl" example:"0.003207"`
	TotalBRL    float64 `json:"total_brl" example:"3.848"`
}

func quoteDTO(q emailcampaign_usecase.PricedQuote) QuoteDTO {
	return QuoteDTO{Units: q.Units, UnitMicros: q.UnitMicros, TotalMicros: q.TotalMicros, UnitBRL: q.UnitBRL, TotalBRL: q.TotalBRL}
}

type AudienceDTO struct {
	Matched     int                `json:"matched"`
	WithEmail   int                `json:"with_email"`
	Sendable    int                `json:"sendable"`
	NeverMailed int                `json:"never_mailed"`
	Skipped     map[string]int     `json:"skipped"`
	Variables   []MergeTagCountDTO `json:"variables"`
	List        *AudienceListDTO   `json:"list,omitempty"`
}

func audienceDTO(a emailcampaign.AudienceSummary) AudienceDTO {
	skipped := make(map[string]int, len(a.Skipped))
	for reason, n := range a.Skipped {
		skipped[string(reason)] = n
	}
	out := AudienceDTO{Matched: a.Matched, WithEmail: a.WithEmail, Sendable: a.Sendable, NeverMailed: a.NeverMailed, Skipped: skipped, Variables: []MergeTagCountDTO{}}
	for _, v := range a.Variables {
		out.Variables = append(out.Variables, MergeTagCountDTO{Key: v.Key, Missing: v.Missing, Policy: string(v.Policy), Text: v.Text, Fallback: v.Fallback})
	}
	if a.List != nil {
		out.List = &AudienceListDTO{ID: a.List.ID, Rows: a.List.Rows, Existing: a.List.Existing, New: a.List.New}
	}
	return out
}

type ReviewResponse struct {
	Audience              AudienceDTO                `json:"audience"`
	Send                  QuoteDTO                   `json:"send"`
	FirstBatch            QuoteDTO                   `json:"first_batch"`
	BatchSize             int64                      `json:"batch_size" example:"1000"`
	BalanceMicros         int64                      `json:"balance_micros"`
	MissingMicros         int64                      `json:"missing_micros"`
	AfterFirstBatchMicros int64                      `json:"after_first_batch_micros" example:"9300400"`
	ExchangeRate          float64                    `json:"exchange_rate" example:"5.5"`
	BalanceBRL            float64                    `json:"balance_brl"`
	MissingBRL            float64                    `json:"missing_brl"`
	AfterFirstBatchBRL    float64                    `json:"after_first_batch_brl" example:"51.1522"`
	TestBatch             int                        `json:"test_batch" example:"200"`
	TimeZone              string                     `json:"time_zone" example:"America/Sao_Paulo"`
	Check                 emailcampaign.LaunchReport `json:"check"`
}

func reviewResponse(v emailcampaign_usecase.ReviewView) ReviewResponse {
	return ReviewResponse{
		Audience: audienceDTO(v.Audience), Send: quoteDTO(v.Send), FirstBatch: quoteDTO(v.FirstBatch), BatchSize: v.BatchSize,
		BalanceMicros: v.BalanceMicros, MissingMicros: v.MissingMicros, AfterFirstBatchMicros: v.AfterFirstBatchMicros, ExchangeRate: v.ExchangeRate,
		BalanceBRL: v.BalanceBRL, MissingBRL: v.MissingBRL, AfterFirstBatchBRL: v.AfterFirstBatchBRL, TimeZone: v.TimeZone,
		TestBatch: v.TestBatch,
		Check:     v.Check,
	}
}

type DesignRequest struct {
	Name         string          `json:"name" example:"Boas-vindas"`
	Kind         string          `json:"kind" enums:"template,campaign" example:"template"`
	ThumbnailURL string          `json:"thumbnail_url,omitempty"`
	Document     json.RawMessage `json:"document" swaggertype:"object"`
}

func (r DesignRequest) input() (emailcampaign.DesignInput, error) {
	doc, err := decodeDocument(r.Document)
	if err != nil {
		return emailcampaign.DesignInput{}, err
	}
	return emailcampaign.DesignInput{Name: r.Name, Kind: emailcampaign.DesignKind(r.Kind), ThumbnailURL: r.ThumbnailURL, Document: doc}, nil
}

type DesignResponse struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Kind         string               `json:"kind"`
	ThumbnailURL string               `json:"thumbnail_url,omitempty"`
	Document     emaildesign.Document `json:"document"`
	CreatedBy    string               `json:"created_by,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

func designResponse(d emailcampaign.Design) DesignResponse {
	return DesignResponse{ID: d.ID, Name: d.Name, Kind: string(d.Kind), ThumbnailURL: d.ThumbnailURL, Document: d.Document, CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
}

type DesignListResponse struct {
	Items    []DesignResponse `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type StarterListResponse struct {
	Items []emaildesign.Starter `json:"items"`
}

type PersonalizationDTO struct {
	FirstName string            `json:"first_name,omitempty"`
	Name      string            `json:"name,omitempty"`
	Email     string            `json:"email,omitempty"`
	Company   string            `json:"company,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type RenderRequest struct {
	Document json.RawMessage     `json:"document" swaggertype:"object"`
	Subject  string              `json:"subject,omitempty"`
	Sample   *PersonalizationDTO `json:"sample,omitempty"`
}

func (r RenderRequest) input() (emailcampaign_usecase.PreviewInput, error) {
	doc, err := decodeDocument(r.Document)
	if err != nil {
		return emailcampaign_usecase.PreviewInput{}, err
	}
	in := emailcampaign_usecase.PreviewInput{Document: doc, Subject: r.Subject}
	if r.Sample != nil {
		in.Sample = &emaildesign.Personalization{FirstName: r.Sample.FirstName, Name: r.Sample.Name, Email: r.Sample.Email, Company: r.Sample.Company, Fields: r.Sample.Fields}
	}
	return in, nil
}

type TestSendRequest struct {
	To []string `json:"to" example:"eu@minhaloja.com.br"`
}

type TestSendResponse struct {
	Results []emailcampaign_usecase.TestResult `json:"results"`
}

type LaunchRequest struct {
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
}

type RecipientResponse struct {
	ID            string     `json:"id"`
	LeadID        string     `json:"lead_id,omitempty"`
	LeadName      string     `json:"lead_name,omitempty"`
	Address       string     `json:"address"`
	Variant       string     `json:"variant,omitempty"`
	Status        string     `json:"status" enums:"pending,skipped,sent,failed"`
	SkipReason    string     `json:"skip_reason,omitempty"`
	FailureCode   string     `json:"failure_code,omitempty"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	MessageStatus string     `json:"message_status,omitempty"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
	OpenedAt      *time.Time `json:"opened_at,omitempty"`
	ClickedAt     *time.Time `json:"clicked_at,omitempty"`
	BouncedAt     *time.Time `json:"bounced_at,omitempty"`
	BounceKind    string     `json:"bounce_kind,omitempty"`
	ComplainedAt  *time.Time `json:"complained_at,omitempty"`
	Unsubscribed  *time.Time `json:"unsubscribed_at,omitempty"`
}

type RecipientListResponse struct {
	Items    []RecipientResponse `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func recipientList(p emailcampaign_usecase.RecipientPage) RecipientListResponse {
	out := RecipientListResponse{Items: make([]RecipientResponse, 0, len(p.Items)), Total: p.Total, Page: p.Page, PageSize: p.PageSize}
	for _, r := range p.Items {
		out.Items = append(out.Items, RecipientResponse{
			ID: r.ID, LeadID: r.LeadID, LeadName: r.LeadName, Address: string(r.Address), Variant: string(r.Variant), Status: string(r.Status),
			SkipReason: string(r.SkipReason), FailureCode: r.FailureCode, SentAt: r.SentAt, MessageStatus: r.MessageStatus,
			DeliveredAt: r.DeliveredAt, OpenedAt: r.FirstOpenAt, ClickedAt: r.FirstClickAt, BouncedAt: r.BouncedAt,
			BounceKind: r.BounceKind, ComplainedAt: r.ComplainedAt, Unsubscribed: r.Unsubscribed,
		})
	}
	return out
}

type LinkListResponse struct {
	Items []emailcampaign.LinkStat `json:"items"`
}

type DomainListResponse struct {
	Items []emailcampaign.DomainStat `json:"items"`
}

type SubscribeRequest struct {
	Address     string     `json:"address" example:"ana@example.com"`
	LeadID      string     `json:"lead_id,omitempty"`
	Source      string     `json:"source" enums:"operator,form,api" example:"operator"`
	IP          string     `json:"ip,omitempty" example:"203.0.113.9"`
	ConsentedAt *time.Time `json:"consented_at,omitempty"`
}

type SubscriptionResponse struct {
	Address           string     `json:"address"`
	LeadID            string     `json:"lead_id,omitempty"`
	Status            string     `json:"status" enums:"subscribed,unsubscribed"`
	Source            string     `json:"source,omitempty"`
	ConsentedAt       *time.Time `json:"consented_at,omitempty"`
	ConsentIP         string     `json:"consent_ip,omitempty"`
	UnsubscribedAt    *time.Time `json:"unsubscribed_at,omitempty"`
	UnsubscribeSource string     `json:"unsubscribe_source,omitempty"`
}

func subscriptionResponse(s emailconsent.Subscription) SubscriptionResponse {
	out := SubscriptionResponse{Address: string(s.Address), LeadID: s.LeadID, Status: string(s.Status), Source: string(s.Source),
		ConsentIP: s.ConsentIP, UnsubscribedAt: s.UnsubscribedAt, UnsubscribeSource: string(s.UnsubscribeSource)}
	if !s.ConsentedAt.IsZero() {
		at := s.ConsentedAt
		out.ConsentedAt = &at
	}
	return out
}

type UnsubscribeAddressRequest struct {
	Address string `json:"address" example:"ana@example.com"`
}

type ImportRowDTO struct {
	Address     string    `json:"address" example:"ana@example.com"`
	LeadID      string    `json:"lead_id,omitempty"`
	IP          string    `json:"ip,omitempty"`
	ConsentedAt time.Time `json:"consented_at"`
}

type ImportRequest struct {
	Rows []ImportRowDTO `json:"rows"`
}

func (r ImportRequest) rows() []emailconsent_usecase.ImportRow {
	out := make([]emailconsent_usecase.ImportRow, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = emailconsent_usecase.ImportRow{Address: row.Address, LeadID: row.LeadID, IP: row.IP, ConsentedAt: row.ConsentedAt}
	}
	return out
}
