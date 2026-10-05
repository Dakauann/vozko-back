package analytics

import (
	"context"
	"strings"
	"time"
)

const invoiceTolerancePct = 5.0

const (
	metaCategoryService = "SERVICE"
	metaPricingRegular  = "REGULAR"
)

type InvoiceState string

const (
	InvoiceMatched     InvoiceState = "matched"
	InvoiceToCheck     InvoiceState = "check"
	InvoiceUnavailable InvoiceState = "unavailable"
)

type InvoiceReason string

const (
	ReasonDialog360      InvoiceReason = "dialog360"
	ReasonNoToken        InvoiceReason = "no_token"
	ReasonMetaUnreadable InvoiceReason = "meta_unreadable"
)

type MetaVolumePoint struct {
	Category    string
	PricingType string
	Volume      int64
}

type MetaVolumes struct {
	Templates        int64
	ChargedTemplates int64
	ChargedService   int64
	FreeService      int64
}

func SumMetaVolumes(points []MetaVolumePoint) MetaVolumes {
	var v MetaVolumes
	for _, p := range points {
		charged := strings.EqualFold(p.PricingType, metaPricingRegular)
		if strings.EqualFold(p.Category, metaCategoryService) {
			if charged {
				v.ChargedService += p.Volume
			} else {
				v.FreeService += p.Volume
			}
			continue
		}
		v.Templates += p.Volume
		if charged {
			v.ChargedTemplates += p.Volume
		}
	}
	return v
}

type MetaPricingAnalytics interface {
	Volumes(ctx context.Context, wabaID, accessToken string, start, end time.Time) ([]MetaVolumePoint, error)
}

type InvoiceAccount struct {
	WABAID          string
	Name            string
	Provider        string
	AccessToken     string
	TemplateSends   int64
	ServiceMessages int64
}

func (a InvoiceAccount) Active() bool {
	return a.TemplateSends != 0 || a.ServiceMessages > 0
}

func SplitActive(accounts []InvoiceAccount) ([]InvoiceAccount, int) {
	active := make([]InvoiceAccount, 0, len(accounts))
	for _, a := range accounts {
		if a.Active() {
			active = append(active, a)
		}
	}
	return active, len(accounts) - len(active)
}

func (a InvoiceAccount) Unreadable() (InvoiceReason, bool) {
	switch {
	case a.Provider == string(ServiceMessageProviderDialog360):
		return ReasonDialog360, true
	case strings.TrimSpace(a.AccessToken) == "":
		return ReasonNoToken, true
	}
	return "", false
}

type WABAInvoiceCheck struct {
	WABAID               string        `json:"wabaId"`
	Name                 string        `json:"name"`
	Provider             string        `json:"provider"`
	OurTemplates         int64         `json:"ourTemplates"`
	MetaTemplates        int64         `json:"metaTemplates"`
	MetaChargedTemplates int64         `json:"metaChargedTemplates"`
	MetaChargedService   int64         `json:"metaChargedService"`
	MetaFreeService      int64         `json:"metaFreeService"`
	Difference           int64         `json:"difference"`
	DifferencePct        *float64      `json:"differencePct,omitempty"`
	State                InvoiceState  `json:"state"`
	Reason               InvoiceReason `json:"reason,omitempty"`
}

func UnavailableInvoice(a InvoiceAccount, reason InvoiceReason) WABAInvoiceCheck {
	return WABAInvoiceCheck{WABAID: a.WABAID, Name: a.Name, Provider: a.Provider, OurTemplates: a.TemplateSends, State: InvoiceUnavailable, Reason: reason}
}

func CompareInvoice(a InvoiceAccount, meta MetaVolumes) WABAInvoiceCheck {
	check := WABAInvoiceCheck{
		WABAID: a.WABAID, Name: a.Name, Provider: a.Provider,
		OurTemplates: a.TemplateSends, MetaTemplates: meta.Templates, MetaChargedTemplates: meta.ChargedTemplates,
		MetaChargedService: meta.ChargedService, MetaFreeService: meta.FreeService,
		Difference: meta.Templates - a.TemplateSends,
		State:      InvoiceMatched,
	}
	if a.TemplateSends == 0 && meta.Templates == 0 {
		return check
	}
	base := a.TemplateSends
	if base == 0 {
		base = meta.Templates
	}
	pct := float64(check.Difference) / float64(base) * 100
	check.DifferencePct = &pct
	if abs(pct) > invoiceTolerancePct {
		check.State = InvoiceToCheck
	}
	return check
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

type InvoiceTotals struct {
	MetaChargedService  int64 `json:"metaChargedService"`
	MetaFreeService     int64 `json:"metaFreeService"`
	UnavailableAccounts int   `json:"unavailableAccounts"`
	IdleAccounts        int   `json:"idleAccounts"`
}

func SumInvoice(checks []WABAInvoiceCheck) InvoiceTotals {
	var totals InvoiceTotals
	for _, c := range checks {
		if c.State == InvoiceUnavailable {
			totals.UnavailableAccounts++
			continue
		}
		totals.MetaChargedService += c.MetaChargedService
		totals.MetaFreeService += c.MetaFreeService
	}
	return totals
}

type MetaInvoiceCheckReport struct {
	Period   Period             `json:"period"`
	Totals   InvoiceTotals      `json:"totals"`
	Accounts []WABAInvoiceCheck `json:"accounts"`
}

type MetaInvoiceCheckRepository interface {
	InvoiceAccounts(start, end time.Time) ([]InvoiceAccount, error)
}

type GetMetaInvoiceCheckUseCase interface {
	Execute(ctx context.Context, start, end time.Time) (*MetaInvoiceCheckReport, error)
}
