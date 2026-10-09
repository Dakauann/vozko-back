package copilot

import (
	"encoding/json"
	"fmt"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

const (
	MaxViewSelectedLeads = 10_000_000
	MaxViewFilterBytes   = 16 << 10
)

func (v View) OnLeads() bool {
	return v.Surface == SurfaceLeads
}

func (v View) validateLeads() error {
	if (View{Surface: v.Surface, LeadFilter: v.LeadFilter, SelectedLeads: v.SelectedLeads}) != v {
		return fmt.Errorf("%w: attendance or studio filters on the leads surface", ErrInvalidView)
	}
	if v.SelectedLeads < 0 || v.SelectedLeads > MaxViewSelectedLeads {
		return fmt.Errorf("%w: the selection count is out of range", ErrInvalidView)
	}
	if v.LeadFilter == nil {
		return nil
	}
	encoded, err := json.Marshal(v.LeadFilter)
	if err != nil || len(encoded) > MaxViewFilterBytes {
		return fmt.Errorf("%w: the lead filter is too large", ErrInvalidView)
	}
	if err := lead.ValidateFilter(*v.LeadFilter); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidView, err)
	}
	return nil
}

func (v View) ScreenLeadFilter() (crmfilter.Filter, bool) {
	if !v.OnLeads() {
		return crmfilter.Filter{}, false
	}
	if v.LeadFilter == nil {
		return crmfilter.Filter{}, true
	}
	return crmfilter.Filter{Groups: append([]crmfilter.Group(nil), v.LeadFilter.Groups...)}, true
}

func (v View) LeadFilterFields() []crmfilter.Field {
	if v.LeadFilter == nil {
		return nil
	}
	return v.LeadFilter.Fields()
}
