package carriercampaign

import (
	"strings"
	"time"

	"vozko/delivery/http/httpx"
	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/carriercampaign"
	cm "vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	ccuc "vozko/usecases/carriercampaign"
)

type VariantDTO struct {
	Name     string                     `json:"name"`
	Content  *rcs.Content               `json:"content,omitempty"`
	Text     string                     `json:"text,omitempty"`
	Bindings []campaign.VariableBinding `json:"bindings"`
}

type WhatsAppFallbackDTO struct {
	BusinessPhoneID string                     `json:"business_phone_id"`
	TemplateID      string                     `json:"template_id"`
	BodyBindings    []campaign.VariableBinding `json:"body_bindings"`
	HeaderBindings  []campaign.VariableBinding `json:"header_bindings"`
}

type SMSFallbackDTO struct {
	RouteID  string                     `json:"route_id"`
	Text     string                     `json:"text"`
	Bindings []campaign.VariableBinding `json:"bindings"`
}

type FallbackDTO struct {
	WhatsApp *WhatsAppFallbackDTO `json:"whatsapp,omitempty"`
	SMS      *SMSFallbackDTO      `json:"sms,omitempty"`
}

type CampaignRequest struct {
	Name        string                     `json:"name" example:"Black Friday"`
	Channel     string                     `json:"channel" example:"rcs"`
	Kind        string                     `json:"kind" example:"marketing"`
	SenderID    string                     `json:"sender_id"`
	Variants    []VariantDTO               `json:"variants"`
	Fallback    FallbackDTO                `json:"fallback"`
	Audience    httpx.LeadSelectionRequest `json:"audience"`
	ScheduledAt *time.Time                 `json:"scheduled_at"`
	Recurrence  *RecurrenceDTO             `json:"recurrence"`
}

type CampaignDTO struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Channel     string                     `json:"channel" example:"rcs"`
	Kind        string                     `json:"kind" example:"marketing"`
	SenderID    string                     `json:"sender_id"`
	Status      string                     `json:"status" example:"draft"`
	Variants    []VariantDTO               `json:"variants"`
	Fallback    FallbackDTO                `json:"fallback"`
	Audience    httpx.LeadSelectionRequest `json:"audience"`
	ScheduledAt *time.Time                 `json:"scheduled_at"`
	Recurrence  *RecurrenceDTO             `json:"recurrence"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
}

type CampaignStatsDTO struct {
	Targeted  int64 `json:"targeted"`
	Sent      int64 `json:"sent"`
	Delivered int64 `json:"delivered"`
	Read      int64 `json:"read"`
	Clicks    int64 `json:"clicks"`
	Replies   int64 `json:"replies"`
}

type CampaignSummaryDTO struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Channel     string           `json:"channel"`
	Status      string           `json:"status"`
	Kind        string           `json:"kind"`
	SenderID    string           `json:"sender_id"`
	ScheduledAt *time.Time       `json:"scheduled_at"`
	Recurrence  *RecurrenceDTO   `json:"recurrence"`
	CreatedAt   time.Time        `json:"created_at"`
	Stats       CampaignStatsDTO `json:"stats"`
}

type CampaignListResponse struct {
	Items []CampaignSummaryDTO `json:"items"`
	Total int64                `json:"total"`
}

type DuplicateRequest struct {
	Name string `json:"name" example:"Black Friday 2"`
}

type TestSendRequest struct {
	Phone   string `json:"phone" example:"+5511999990000"`
	Variant int    `json:"variant" example:"0"`
}

type ReadyDTO struct {
	Sender  bool   `json:"sender"`
	Route   bool   `json:"route"`
	Blocker string `json:"blocker,omitempty" example:"route_not_ready"`
}

type EstimateDTO struct {
	Channel             string                  `json:"channel"`
	Recipients          int                     `json:"recipients"`
	AudienceFingerprint string                  `json:"audience_fingerprint"`
	Messages            int64                   `json:"messages"`
	WorstCaseSegments   int                     `json:"worst_case_segments,omitempty"`
	WorstCaseMicros     int64                   `json:"worst_case_micros"`
	ExpectedMicros      int64                   `json:"expected_micros"`
	PerRecipientMicros  int64                   `json:"per_recipient_micros"`
	ShortfallMicros     int64                   `json:"shortfall_micros"`
	Ready               ReadyDTO                `json:"ready"`
	Count               *campaignaudience.Count `json:"count"`
}

type TestSendDTO struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code"`
	Class     string `json:"class,omitempty"`
	Segments  int    `json:"segments,omitempty"`
}

type RatesDTO struct {
	Delivery                *float64 `json:"delivery"`
	Read                    *float64 `json:"read"`
	TapThrough              *float64 `json:"tap_through"`
	ClickThrough            *float64 `json:"click_through"`
	Reply                   *float64 `json:"reply"`
	OptOut                  *float64 `json:"opt_out"`
	Fallback                *float64 `json:"fallback"`
	Conversion              *float64 `json:"conversion"`
	CostPerConversionMicros *int64   `json:"cost_per_conversion_micros"`
}

type FunnelCountsDTO struct {
	Targeted              int64 `json:"targeted"`
	Skipped               int64 `json:"skipped"`
	Sent                  int64 `json:"sent"`
	Accepted              int64 `json:"accepted"`
	Rejected              int64 `json:"rejected"`
	Delivered             int64 `json:"delivered"`
	Read                  int64 `json:"read"`
	Failed                int64 `json:"failed"`
	Expired               int64 `json:"expired"`
	Taps                  int64 `json:"taps"`
	Clicks                int64 `json:"clicks"`
	Replies               int64 `json:"replies"`
	OptOuts               int64 `json:"opt_outs"`
	FellBackWhatsApp      int64 `json:"fell_back_whatsapp"`
	FellBackSMS           int64 `json:"fell_back_sms"`
	Conversions           int64 `json:"conversions"`
	ConversionValueMicros int64 `json:"conversion_value_micros"`
	SpendMicros           int64 `json:"spend_micros"`
}

type FunnelDTO struct {
	FunnelCountsDTO
	Rates RatesDTO `json:"rates"`
}

type VariantStatDTO struct {
	Variant int             `json:"variant"`
	Name    string          `json:"name"`
	Funnel  FunnelCountsDTO `json:"funnel"`
	Rates   RatesDTO        `json:"rates"`
}

type ButtonStatDTO struct {
	Card   int    `json:"card"`
	Index  int    `json:"index"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	Taps   int64  `json:"taps"`
	Clicks int64  `json:"clicks"`
}

type CarrierStatDTO struct {
	Operator     string   `json:"operator"`
	Sent         int64    `json:"sent"`
	Delivered    int64    `json:"delivered"`
	FellBack     int64    `json:"fell_back"`
	FallbackRate *float64 `json:"fallback_rate"`
}

type TimelineDTO struct {
	Bucket string                   `json:"bucket" example:"hour"`
	Points []campaign.TimelinePoint `json:"points"`
}

type RecipientDTO struct {
	Phone         string    `json:"phone"`
	LeadID        string    `json:"lead_id"`
	LeadName      string    `json:"lead_name"`
	Variant       int       `json:"variant"`
	Status        string    `json:"status"`
	SkipReason    string    `json:"skip_reason"`
	MessageStatus string    `json:"message_status"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RecipientListResponse struct {
	Items []RecipientDTO `json:"items"`
	Total int64          `json:"total"`
}

type OverviewTotalsDTO struct {
	Sent                  int64 `json:"sent"`
	Delivered             int64 `json:"delivered"`
	Read                  int64 `json:"read"`
	Failed                int64 `json:"failed"`
	Taps                  int64 `json:"taps"`
	Clicks                int64 `json:"clicks"`
	Replies               int64 `json:"replies"`
	OptOuts               int64 `json:"opt_outs"`
	FellBackWhatsApp      int64 `json:"fell_back_whatsapp"`
	FellBackSMS           int64 `json:"fell_back_sms"`
	Conversions           int64 `json:"conversions"`
	ConversionValueMicros int64 `json:"conversion_value_micros"`
	SpendMicros           int64 `json:"spend_micros"`
}

type OverviewDayDTO struct {
	Day         string `json:"day" example:"2026-10-21"`
	Channel     string `json:"channel"`
	Sent        int64  `json:"sent"`
	Delivered   int64  `json:"delivered"`
	Read        int64  `json:"read"`
	Failed      int64  `json:"failed"`
	SpendMicros int64  `json:"spend_micros"`
}

type ChannelCostDTO struct {
	Channel                 string `json:"channel" example:"rcs"`
	SpendMicros             int64  `json:"spend_micros"`
	Conversions             int64  `json:"conversions"`
	CostPerConversionMicros *int64 `json:"cost_per_conversion_micros"`
}

type OverviewDTO struct {
	Totals   OverviewTotalsDTO `json:"totals"`
	Rates    RatesDTO          `json:"rates"`
	Daily    []OverviewDayDTO  `json:"daily"`
	Channels []ChannelCostDTO  `json:"channels"`
	Carriers []CarrierStatDTO  `json:"carriers"`
}

func bindings(in []campaign.VariableBinding) []campaign.VariableBinding {
	if in == nil {
		return []campaign.VariableBinding{}
	}
	return in
}

func (req CampaignRequest) draft() (ccuc.Draft, error) {
	recurrence, err := req.Recurrence.domain()
	if err != nil {
		return ccuc.Draft{}, err
	}
	channel := cm.Channel(strings.TrimSpace(req.Channel))
	variants := make([]carriercampaign.Variant, 0, len(req.Variants))
	for _, v := range req.Variants {
		variant := carriercampaign.Variant{Name: v.Name, Text: v.Text, Bindings: v.Bindings}
		if v.Content != nil {
			variant.Content = *v.Content
		}
		variants = append(variants, variant)
	}
	var fallback carriercampaign.Fallback
	if wa := req.Fallback.WhatsApp; wa != nil {
		fallback.WhatsApp = &carriercampaign.WhatsAppFallback{BusinessPhoneID: wa.BusinessPhoneID, TemplateID: wa.TemplateID, BodyBindings: wa.BodyBindings, HeaderBindings: wa.HeaderBindings}
	}
	if s := req.Fallback.SMS; s != nil {
		fallback.SMS = &carriercampaign.SMSFallback{RouteID: s.RouteID, Text: s.Text, Bindings: s.Bindings}
	}
	return ccuc.Draft{
		Name: req.Name, SenderID: req.SenderID, Channel: channel, Kind: carriercampaign.Kind(strings.TrimSpace(req.Kind)),
		Variants: variants, Fallback: fallback, Audience: req.Audience.Selection(),
		ScheduledAt: req.ScheduledAt, Recurrence: recurrence,
	}, nil
}

func campaignDTO(c *carriercampaign.Campaign) CampaignDTO {
	variants := make([]VariantDTO, 0, len(c.Variants))
	for _, v := range c.Variants {
		dto := VariantDTO{Name: v.Name, Text: v.Text, Bindings: bindings(v.Bindings)}
		if c.Channel == cm.ChannelRCS {
			content := v.Content
			dto.Content = &content
		}
		variants = append(variants, dto)
	}
	var fallback FallbackDTO
	if wa := c.Fallback.WhatsApp; wa != nil {
		fallback.WhatsApp = &WhatsAppFallbackDTO{BusinessPhoneID: wa.BusinessPhoneID, TemplateID: wa.TemplateID, BodyBindings: bindings(wa.BodyBindings), HeaderBindings: bindings(wa.HeaderBindings)}
	}
	if s := c.Fallback.SMS; s != nil {
		fallback.SMS = &SMSFallbackDTO{RouteID: s.RouteID, Text: s.Text, Bindings: bindings(s.Bindings)}
	}
	return CampaignDTO{
		ID: c.ID, Name: c.Name, Channel: string(c.Channel), Kind: string(c.Kind), SenderID: c.SenderID, Status: string(c.Status),
		Variants: variants, Fallback: fallback, Audience: httpx.LeadSelectionRequestOf(c.Audience), ScheduledAt: c.ScheduledAt, Recurrence: recurrenceDTO(c.Recurrence),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func summaryDTO(s ccuc.CampaignSummary) CampaignSummaryDTO {
	c, f := s.Campaign, s.Stats
	return CampaignSummaryDTO{
		ID: c.ID, Name: c.Name, Channel: string(c.Channel), Status: string(c.Status), Kind: string(c.Kind), SenderID: c.SenderID,
		ScheduledAt: c.ScheduledAt, Recurrence: recurrenceDTO(c.Recurrence), CreatedAt: c.CreatedAt,
		Stats: CampaignStatsDTO{Targeted: f.Targeted, Sent: f.Sent, Delivered: f.Delivered, Read: f.Read, Clicks: f.Clicks, Replies: f.Replies},
	}
}

func estimateDTO(e ccuc.EstimateResult) EstimateDTO {
	return EstimateDTO{
		Channel: string(e.Estimate.Channel), Recipients: e.Audience, AudienceFingerprint: e.AudienceFingerprint, Messages: e.Estimate.Messages,
		WorstCaseSegments: e.Estimate.WorstCaseSegments, WorstCaseMicros: e.Estimate.WorstCaseMicros,
		ExpectedMicros: e.Estimate.ExpectedMicros, PerRecipientMicros: e.Estimate.PerRecipientMicros,
		ShortfallMicros: e.ShortfallMicros,
		Ready:           ReadyDTO{Sender: e.Readiness.Sender, Route: e.Readiness.Route, Blocker: e.Readiness.Blocker},
		Count:           e.Count,
	}
}

func testSendDTO(m *cm.Message) TestSendDTO {
	return TestSendDTO{
		ID: m.ID, Channel: string(m.Channel), Reference: m.Reference, Status: string(m.Status),
		ErrorCode: m.ErrorCode, Class: string(m.Class), Segments: m.Segments,
	}
}

func ratesDTO(r carriercampaign.Rates) RatesDTO {
	return RatesDTO{
		Delivery: r.Delivery, Read: r.Read, TapThrough: r.TapThrough, ClickThrough: r.ClickThrough, Reply: r.Reply,
		OptOut: r.OptOut, Fallback: r.Fallback, Conversion: r.Conversion, CostPerConversionMicros: r.CostPerConversionMicros,
	}
}

func countsDTO(f carriercampaign.Funnel) FunnelCountsDTO {
	return FunnelCountsDTO{
		Targeted: f.Targeted, Skipped: f.Skipped, Sent: f.Sent, Accepted: f.Accepted, Rejected: f.Rejected,
		Delivered: f.Delivered, Read: f.Read, Failed: f.Failed, Expired: f.Expired, Taps: f.Taps, Clicks: f.Clicks,
		Replies: f.Replies, OptOuts: f.OptOuts, FellBackWhatsApp: f.FellBackWhatsApp, FellBackSMS: f.FellBackSMS,
		Conversions: f.Conversions, ConversionValueMicros: f.ConversionValueMicros, SpendMicros: f.SpendMicros,
	}
}

func funnelDTO(v ccuc.FunnelView) FunnelDTO {
	return FunnelDTO{FunnelCountsDTO: countsDTO(v.Funnel), Rates: ratesDTO(v.Rates)}
}

func carrierDTOs(stats []carriercampaign.CarrierStat) []CarrierStatDTO {
	out := make([]CarrierStatDTO, 0, len(stats))
	for _, c := range stats {
		out = append(out, CarrierStatDTO{Operator: c.Operator, Sent: c.Sent, Delivered: c.Delivered, FellBack: c.FellBack, FallbackRate: c.FallbackRate()})
	}
	return out
}

func recipientDTO(r ccuc.RecipientRow) RecipientDTO {
	rec := r.Recipient
	return RecipientDTO{
		Phone: rec.Phone, LeadID: rec.LeadID, LeadName: r.LeadName, Variant: rec.Variant, Status: string(rec.Status),
		SkipReason: string(rec.SkipReason), MessageStatus: string(r.MessageStatus), UpdatedAt: rec.UpdatedAt,
	}
}

func overviewDTO(o ccuc.OverviewResult) OverviewDTO {
	t := o.Totals
	out := OverviewDTO{
		Totals: OverviewTotalsDTO{
			Sent: t.Sent, Delivered: t.Delivered, Read: t.Read, Failed: t.Failed, Taps: t.Taps, Clicks: t.Clicks,
			Replies: t.Replies, OptOuts: t.OptOuts, FellBackWhatsApp: t.FellBackWhatsApp, FellBackSMS: t.FellBackSMS,
			Conversions: t.Conversions, ConversionValueMicros: t.ConversionValueMicros, SpendMicros: t.SpendMicros,
		},
		Rates:    ratesDTO(o.Rates),
		Daily:    make([]OverviewDayDTO, 0, len(o.Daily)),
		Channels: make([]ChannelCostDTO, 0, len(o.Channels)),
		Carriers: carrierDTOs(o.Carriers),
	}
	for _, d := range o.Daily {
		out.Daily = append(out.Daily, OverviewDayDTO{Day: d.Day.String(), Channel: string(d.Channel), Sent: d.Sent, Delivered: d.Delivered, Read: d.Read, Failed: d.Failed, SpendMicros: d.SpendMicros})
	}
	for _, c := range o.Channels {
		out.Channels = append(out.Channels, ChannelCostDTO{Channel: c.Channel, SpendMicros: c.SpendMicros, Conversions: c.Conversions, CostPerConversionMicros: c.CostPerConversionMicros})
	}
	return out
}

type RecurrenceDTO struct {
	Frequency  string  `json:"frequency" example:"weekly"`
	Time       string  `json:"time" example:"09:00"`
	Weekdays   []int   `json:"weekdays,omitempty"`
	DayOfMonth int     `json:"day_of_month,omitempty"`
	Timezone   string  `json:"timezone" example:"America/Sao_Paulo"`
	Until      *string `json:"until,omitempty" example:"2026-12-31"`
}

func (r *RecurrenceDTO) domain() (*carriercampaign.Recurrence, error) {
	if r == nil {
		return nil, nil
	}
	out := carriercampaign.Recurrence{
		Frequency: carriercampaign.Frequency(strings.TrimSpace(r.Frequency)), Time: r.Time,
		DayOfMonth: r.DayOfMonth, Timezone: r.Timezone,
	}
	for _, d := range r.Weekdays {
		out.Weekdays = append(out.Weekdays, time.Weekday(d))
	}
	if r.Until != nil {
		until, err := shared.ParseDate(*r.Until)
		if err != nil {
			return nil, err
		}
		out.Until = &until
	}
	return &out, nil
}

func recurrenceDTO(r *carriercampaign.Recurrence) *RecurrenceDTO {
	if r == nil {
		return nil
	}
	out := RecurrenceDTO{Frequency: string(r.Frequency), Time: r.Time, DayOfMonth: r.DayOfMonth, Timezone: r.Timezone}
	for _, d := range r.Weekdays {
		out.Weekdays = append(out.Weekdays, int(d))
	}
	if r.Until != nil {
		until := r.Until.String()
		out.Until = &until
	}
	return &out
}
