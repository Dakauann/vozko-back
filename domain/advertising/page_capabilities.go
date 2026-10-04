package advertising

import "net/url"

type PageChannel string

const (
	PageAdvertise PageChannel = "advertise"
	PageWhatsApp  PageChannel = "whatsapp"
	PageInstagram PageChannel = "instagram"
	PageMessenger PageChannel = "messenger"
	PageLeadForms PageChannel = "lead_forms"
)

const (
	ActionLinkWhatsApp    InAppAction = "link_whatsapp"
	ActionConnectWhatsApp InAppAction = "connect_whatsapp"
)

const (
	pageHomePortalBase  = "https://business.facebook.com/latest/home"
	PageAccessPortalURL = "https://www.facebook.com/settings/?tab=profile_access"
	LeadTermsPortalURL  = "https://www.facebook.com/ads/leadgen/tos"
	pageRolesPortalBase = "https://business.facebook.com/latest/settings/pages"
)

type PageCapability struct {
	Channel PageChannel
	State   ReadinessState
	Action  ReadinessAction
}

func PageCapabilities(page RemotePage, numbers []WorkspaceNumber) []PageCapability {
	return []PageCapability{
		pageCapability(PageAdvertise, page.CanAdvertise, ReadinessAction{Portal: pageRolesPortalURL(page)}),
		pageCapability(PageWhatsApp, len(NumbersLinkedTo(page, numbers)) > 0, whatsAppAction(page, numbers)),
		pageCapability(PageInstagram, page.InstagramUserID != "", ReadinessAction{Portal: pageHomePortalURL(page)}),
		pageCapability(PageMessenger, true, ReadinessAction{}),
		pageCapability(PageLeadForms, page.LeadTermsAccepted, ReadinessAction{Portal: LeadTermsPortalURL}),
	}
}

func pageCapability(channel PageChannel, ready bool, fix ReadinessAction) PageCapability {
	c := PageCapability{Channel: channel, State: Observed(ready).state()}
	if !ready {
		c.Action = fix
	}
	return c
}

func whatsAppAction(page RemotePage, numbers []WorkspaceNumber) ReadinessAction {
	if len(NumbersLinkableTo(page, numbers)) > 0 {
		return ReadinessAction{InApp: ActionLinkWhatsApp}
	}
	return ReadinessAction{InApp: ActionConnectWhatsApp}
}

func pageHomePortalURL(page RemotePage) string {
	query := url.Values{"asset_id": {page.PageID}}
	if page.BusinessID != "" {
		query.Set("business_id", page.BusinessID)
	}
	return pageHomePortalBase + "?" + query.Encode()
}

func pageRolesPortalURL(page RemotePage) string {
	if page.BusinessID == "" {
		return PageAccessPortalURL
	}
	return pageRolesPortalBase + "?" + url.Values{"business_id": {page.BusinessID}, "selected_asset_id": {page.PageID}}.Encode()
}
