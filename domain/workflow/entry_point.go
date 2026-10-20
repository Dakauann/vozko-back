package workflow

import (
	"sort"
	"strings"
	"time"
)

type EntryPointKind string

const (
	EntryMessageReceived EntryPointKind = "message_received"
	EntryFirstMessage    EntryPointKind = "first_message"
	EntryWebhook         EntryPointKind = "webhook"
	EntryFormSubmitted   EntryPointKind = "form_submitted"
	EntryAgentCall       EntryPointKind = "agent_call"
	EntryCampaignSent    EntryPointKind = "campaign_sent"
	EntryStageAdded      EntryPointKind = "stage_added"
	EntryNoReply         EntryPointKind = "no_reply"
	EntryCallReceived    EntryPointKind = "call_received"
	EntryManual          EntryPointKind = "manual"
	EntryLeadExit        EntryPointKind = "lead_exit"
)

var entryKindOfTrigger = map[TriggerType]EntryPointKind{
	TriggerMessageReceived: EntryMessageReceived,
	TriggerFirstMessage:    EntryFirstMessage,
	TriggerWebhook:         EntryWebhook,
	TriggerFormSubmitted:   EntryFormSubmitted,
	TriggerAgentCall:       EntryAgentCall,
	TriggerCampaignSent:    EntryCampaignSent,
	TriggerStageAdded:      EntryStageAdded,
	TriggerNoReply:         EntryNoReply,
	TriggerCallReceived:    EntryCallReceived,
	TriggerManual:          EntryManual,
}

func EntryKindOfTrigger(t TriggerType) (EntryPointKind, bool) {
	kind, ok := entryKindOfTrigger[t]
	return kind, ok
}

const (
	RunOriginLeadExit = "exit"
	RunOriginManual   = "manual"
)

func RunOrigins() []string {
	origins := []string{RunOriginLeadExit, RunOriginManual}
	for t := range entryKindOfTrigger {
		origins = append(origins, string(t))
	}
	sort.Strings(origins)
	return origins
}

func EntryKindOfRunOrigin(origin string) (EntryPointKind, bool) {
	switch origin {
	case RunOriginLeadExit:
		return EntryLeadExit, true
	case RunOriginManual:
		return EntryManual, true
	}
	return EntryKindOfTrigger(TriggerType(origin))
}

func RunOriginPrefix(origin string) string { return origin + ":" }

type EntryReadiness string

const (
	EntryReady   EntryReadiness = "ready"
	EntryBlocked EntryReadiness = "blocked"
)

type EntryBlockReason string

const (
	EntryBlockedNotPublished    EntryBlockReason = "not_published"
	EntryBlockedPaused          EntryBlockReason = "paused"
	EntryBlockedWebhookInactive EntryBlockReason = "webhook_inactive"
	EntryBlockedNoWebhook       EntryBlockReason = "no_webhook"
	EntryBlockedNoForm          EntryBlockReason = "no_form"
	EntryBlockedFormInactive    EntryBlockReason = "form_inactive"
	EntryBlockedNoAgentBound    EntryBlockReason = "no_agent_bound"
	EntryBlockedNoChannel       EntryBlockReason = "no_channel"
	EntryBlockedChannelPaused   EntryBlockReason = "channel_paused"
	EntryBlockedNoTrigger       EntryBlockReason = "no_trigger"
)

const CampaignTriggerCampaignIDKey = "campaign_id"

const MaxListedChannels = 10

type EntryChannelKind string

const (
	ChannelWhatsAppCampaign           EntryChannelKind = "whatsapp_campaign"
	ChannelUnofficialWhatsAppCampaign EntryChannelKind = "unofficial_whatsapp_campaign"
	ChannelUnofficialWhatsAppInstance EntryChannelKind = "unofficial_whatsapp_instance"
	ChannelFacebookPage               EntryChannelKind = "facebook_page"
	ChannelInstagramAccount           EntryChannelKind = "instagram_account"
	ChannelTelegramAccount            EntryChannelKind = "telegram_account"
	ChannelTikTokAccount              EntryChannelKind = "tiktok_account"
	ChannelMercadoLivreAccount        EntryChannelKind = "mercadolivre_account"
	ChannelWebchatWidget              EntryChannelKind = "webchat_widget"
)

func (k EntryChannelKind) IsCampaign() bool {
	return k == ChannelWhatsAppCampaign || k == ChannelUnofficialWhatsAppCampaign
}

type LeadExitSource string

const (
	LeadExitFromTemplate LeadExitSource = "template"
	LeadExitFromCampaign LeadExitSource = "campaign"
)

type WebhookEntry struct {
	Active   bool            `json:"active"`
	AuthMode WebhookAuthMode `json:"authMode"`
	Method   string          `json:"method"`
}

type EntryForm struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Active  bool   `json:"active"`
	Missing bool   `json:"missing,omitempty"`
}

type EntryAgent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type EntryChannel struct {
	Kind            EntryChannelKind `json:"kind"`
	ID              string           `json:"id"`
	Name            string           `json:"name,omitempty"`
	WorkflowEnabled bool             `json:"workflowEnabled"`
	Bound           bool             `json:"bound"`
}

type LeadExitEntry struct {
	Source   LeadExitSource `json:"source"`
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Language string         `json:"language,omitempty"`
	Button   string         `json:"button"`
}

type EntryPointDetails struct {
	Webhook   *WebhookEntry   `json:"webhook,omitempty"`
	Forms     []EntryForm     `json:"forms,omitempty"`
	AnyForm   bool            `json:"anyForm,omitempty"`
	Agents    []EntryAgent    `json:"agents,omitempty"`
	Channels  []EntryChannel  `json:"channels,omitempty"`
	LeadExits []LeadExitEntry `json:"leadExits,omitempty"`
}

type EntryPoint struct {
	Kind           EntryPointKind    `json:"kind"`
	NodeID         string            `json:"nodeId,omitempty"`
	Readiness      EntryReadiness    `json:"readiness"`
	Reason         EntryBlockReason  `json:"reason,omitempty"`
	Details        EntryPointDetails `json:"details"`
	LastReceivedAt *time.Time        `json:"lastReceivedAt,omitempty"`
	LastOutcome    WebhookOutcome    `json:"lastOutcome,omitempty"`
	LastReason     string            `json:"lastReason,omitempty"`
}

type EntryDelivery struct {
	At      time.Time
	Outcome WebhookOutcome
	Reason  string
}

type EntryPointFacts struct {
	Webhook             *WebhookEntry
	Forms               map[string]EntryForm
	Agents              []EntryAgent
	Channels            []EntryChannel
	OpenChannels        []EntryChannel
	EnabledCampaigns    []EntryChannel
	ConfiguredCampaigns map[string]EntryChannel
	LeadExits           []LeadExitEntry
	LastStarts          map[EntryPointKind]time.Time
	LastDelivery        *EntryDelivery
}

func WorkflowBlockReason(w *Workflow) EntryBlockReason {
	switch {
	case w == nil || w.PublishedVersion == nil:
		return EntryBlockedNotPublished
	case w.Status != WorkflowStatusActive:
		return EntryBlockedPaused
	}
	return ""
}

func (w *Workflow) Callable() bool {
	return WorkflowBlockReason(w) == ""
}

func EntryPointsOf(w *Workflow, triggers []Node, facts EntryPointFacts) []EntryPoint {
	var out []EntryPoint
	seen := map[EntryPointKind]bool{}
	for _, node := range triggers {
		kind, ok := EntryKindOfTrigger(TriggerType(node.Type))
		if !ok {
			continue
		}
		seen[kind] = true
		out = append(out, triggerEntry(w, node, kind, facts))
	}
	if !seen[EntryWebhook] && facts.Webhook != nil && facts.Webhook.Active {
		out = append(out, bindingEntry(w, EntryWebhook, EntryBlockedNoTrigger, EntryPointDetails{Webhook: facts.Webhook}, facts))
	}
	if !seen[EntryAgentCall] && anyActiveAgent(facts.Agents) {
		out = append(out, bindingEntry(w, EntryAgentCall, EntryBlockedNoTrigger, EntryPointDetails{Agents: facts.Agents}, facts))
	}
	if len(facts.LeadExits) > 0 {
		out = append(out, bindingEntry(w, EntryLeadExit, "", EntryPointDetails{LeadExits: facts.LeadExits}, facts))
	}
	return out
}

func bindingEntry(w *Workflow, kind EntryPointKind, reason EntryBlockReason, details EntryPointDetails, facts EntryPointFacts) EntryPoint {
	if reason == "" {
		reason = WorkflowBlockReason(w)
	}
	return received(EntryPoint{Kind: kind, Details: details}.decided(reason), facts)
}

func triggerEntry(w *Workflow, node Node, kind EntryPointKind, facts EntryPointFacts) EntryPoint {
	details, reason := kindReadiness(node, kind, facts)
	if workflowReason := WorkflowBlockReason(w); workflowReason != "" {
		reason = workflowReason
	}
	return received(EntryPoint{Kind: kind, NodeID: node.ID, Details: details}.decided(reason), facts)
}

func (e EntryPoint) decided(reason EntryBlockReason) EntryPoint {
	e.Reason, e.Readiness = reason, EntryReady
	if reason != "" {
		e.Readiness = EntryBlocked
	}
	return e
}

func received(e EntryPoint, facts EntryPointFacts) EntryPoint {
	if e.Kind == EntryWebhook && facts.LastDelivery != nil {
		at := facts.LastDelivery.At
		e.LastReceivedAt, e.LastOutcome, e.LastReason = &at, facts.LastDelivery.Outcome, facts.LastDelivery.Reason
		return e
	}
	if at, ok := facts.LastStarts[e.Kind]; ok && !at.IsZero() {
		e.LastReceivedAt = &at
	}
	return e
}

func kindReadiness(node Node, kind EntryPointKind, facts EntryPointFacts) (EntryPointDetails, EntryBlockReason) {
	switch kind {
	case EntryWebhook:
		return webhookReadiness(facts)
	case EntryFormSubmitted:
		return formReadiness(node, facts)
	case EntryAgentCall:
		return agentReadiness(facts)
	case EntryMessageReceived, EntryFirstMessage:
		return messageReadiness(facts)
	case EntryCampaignSent:
		return campaignReadiness(node, facts)
	}
	return EntryPointDetails{}, ""
}

func webhookReadiness(facts EntryPointFacts) (EntryPointDetails, EntryBlockReason) {
	details := EntryPointDetails{Webhook: facts.Webhook}
	switch {
	case facts.Webhook == nil:
		return details, EntryBlockedNoWebhook
	case !facts.Webhook.Active:
		return details, EntryBlockedWebhookInactive
	}
	return details, ""
}

func formReadiness(node Node, facts EntryPointFacts) (EntryPointDetails, EntryBlockReason) {
	raw, present := node.Config[FormTriggerFormIDsKey]
	if !present || raw == nil {
		return EntryPointDetails{AnyForm: true}, ""
	}
	ids, readable := formIDList(raw)
	if !readable {
		return EntryPointDetails{}, EntryBlockedNoForm
	}
	if len(ids) == 0 {
		return EntryPointDetails{AnyForm: true}, ""
	}
	details := EntryPointDetails{}
	reason := EntryBlockedNoForm
	for _, id := range ids {
		form, found := facts.Forms[id]
		if !found {
			details.Forms = append(details.Forms, EntryForm{ID: id, Missing: true})
			continue
		}
		details.Forms = append(details.Forms, form)
		switch {
		case form.Active:
			reason = ""
		case reason == EntryBlockedNoForm:
			reason = EntryBlockedFormInactive
		}
	}
	return details, reason
}

func anyActiveAgent(agents []EntryAgent) bool {
	for _, a := range agents {
		if a.Active {
			return true
		}
	}
	return false
}

func agentReadiness(facts EntryPointFacts) (EntryPointDetails, EntryBlockReason) {
	details := EntryPointDetails{Agents: facts.Agents}
	if !anyActiveAgent(facts.Agents) {
		return details, EntryBlockedNoAgentBound
	}
	return details, ""
}

func messageReadiness(facts EntryPointFacts) (EntryPointDetails, EntryBlockReason) {
	return channelReadiness(facts.Channels, facts.OpenChannels, func(EntryChannel) bool { return true })
}

func campaignReadiness(node Node, facts EntryPointFacts) (EntryPointDetails, EntryBlockReason) {
	chosen, _ := node.Config[CampaignTriggerCampaignIDKey].(string)
	if chosen = strings.TrimSpace(chosen); chosen != "" {
		campaign, found := facts.ConfiguredCampaigns[chosen]
		switch {
		case !found:
			return EntryPointDetails{}, EntryBlockedNoChannel
		case !campaign.WorkflowEnabled:
			return EntryPointDetails{Channels: []EntryChannel{campaign}}, EntryBlockedChannelPaused
		}
		return EntryPointDetails{Channels: []EntryChannel{campaign}}, ""
	}
	return channelReadiness(facts.Channels, facts.EnabledCampaigns, func(c EntryChannel) bool { return c.Kind.IsCampaign() })
}

func channelReadiness(bound, others []EntryChannel, counts func(EntryChannel) bool) (EntryPointDetails, EntryBlockReason) {
	var listed []EntryChannel
	listedKeys := map[EntryChannel]bool{}
	reason := EntryBlockedNoChannel
	add := func(c EntryChannel, isBound bool) {
		if !counts(c) {
			return
		}
		c.Bound = isBound
		key := EntryChannel{Kind: c.Kind, ID: c.ID}
		if listedKeys[key] {
			return
		}
		listedKeys[key] = true
		if c.WorkflowEnabled {
			reason = ""
		} else if reason == EntryBlockedNoChannel {
			reason = EntryBlockedChannelPaused
		}
		if len(listed) < MaxListedChannels {
			listed = append(listed, c)
		}
	}
	for _, c := range bound {
		add(c, true)
	}
	for _, c := range others {
		add(c, false)
	}
	return EntryPointDetails{Channels: listed}, reason
}
