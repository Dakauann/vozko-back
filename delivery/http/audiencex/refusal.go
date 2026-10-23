package audiencex

import (
	"context"
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/cache"
	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
	"vozko/domain/lead"
	"vozko/domain/selection"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/usecases/campaignguard"
)

const (
	CodeBusy             = "audience_busy"
	CodeTemplateUnusable = "send_template_unavailable"
	CodeInstanceUnusable = "send_instance_unavailable"
	CodeForbidden        = "forbidden"
	CodeSelectionChanged = "selection_changed"
	retryAfterSeconds    = "5"
	selectionChangedText = "A seleção mudou desde a contagem"
)

var audienceStatuses = map[string]int{
	"audience_list_not_found":             http.StatusNotFound,
	"audience_row_not_found":              http.StatusNotFound,
	"audience_row_duplicate":              http.StatusConflict,
	"audience_list_expired":               http.StatusGone,
	"audience_file_too_large":             http.StatusRequestEntityTooLarge,
	"audience_list_too_large":             http.StatusRequestEntityTooLarge,
	"audience_file_too_wide":              http.StatusRequestEntityTooLarge,
	"audience_list_channel_mismatch":      http.StatusUnprocessableEntity,
	"audience_source_unsupported":         http.StatusUnprocessableEntity,
	"audience_contact_column_missing":     http.StatusUnprocessableEntity,
	"audience_variable_choice_required":   http.StatusUnprocessableEntity,
	"audience_variable_source_required":   http.StatusUnprocessableEntity,
	"audience_list_variables_missing":     http.StatusUnprocessableEntity,
	"audience_list_metadata_missing":      http.StatusUnprocessableEntity,
	"audience_leads_metadata_unavailable": http.StatusUnprocessableEntity,
	"audience_lead_status_invalid":        http.StatusBadRequest,
	"template_suggestion_slot_invalid":    http.StatusUnprocessableEntity,
	"audience_unavailable":                http.StatusServiceUnavailable,
}

var campaignStatuses = map[error]int{
	campaign.ErrCreationScopeMissing:   http.StatusForbidden,
	campaign.ErrLeadTargetsUnavailable: http.StatusServiceUnavailable,
	campaign.ErrBindingsMismatch:       http.StatusBadRequest,
	campaign.ErrBindingUnknown:         http.StatusBadRequest,
	campaign.ErrBindingLiteralEmpty:    http.StatusBadRequest,
	campaign.ErrBindingFieldUnknown:    http.StatusBadRequest,
	campaign.ErrBindingSensitive:       http.StatusBadRequest,
	campaign.ErrWorkflowNotFound:       http.StatusUnprocessableEntity,
	campaign.ErrWorkflowForbidden:      http.StatusForbidden,
	campaign.ErrAgentNotFound:          http.StatusUnprocessableEntity,
	campaign.ErrAutomationUnavailable:  http.StatusServiceUnavailable,
}

var templateErrors = []error{wc.ErrCampaignTemplateNotFound, wc.ErrCampaignTemplateNotApproved, template.ErrTemplateNotSendable, template.ErrTemplateNotFound, template.ErrTemplateAccessDenied}

var instanceErrors = []error{uw.ErrInstanceNotFound, uw.ErrInstanceOutsideDepartment, uw.ErrInstanceNotConnected, uw.ErrRestrictedByWA}

func WriteRefusal(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var changed *selection.CountChangedError
	if errors.As(err, &changed) {
		response.WriteErrorWithDetails(w, http.StatusConflict, CodeSelectionChanged, selectionChangedText, map[string]int{"expected": changed.Expected, "matched": changed.Matched})
		return true
	}
	status, code, ok := refusalOf(err)
	if !ok {
		return false
	}
	if code == CodeBusy {
		w.Header().Set("Retry-After", retryAfterSeconds)
	}
	response.WriteErrorWithCode(w, status, code, err.Error(), nil)
	return true
}

func refusalOf(err error) (int, string, bool) {
	if code := audience.ErrorCode(err); code != "" {
		status, known := audienceStatuses[code]
		if !known {
			status = http.StatusBadRequest
		}
		return status, code, true
	}
	switch {
	case errors.Is(err, cache.ErrGateBusy), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, CodeBusy, true
	case errors.Is(err, campaignguard.ErrUnavailable):
		return http.StatusServiceUnavailable, "audience_unavailable", true
	case errors.Is(err, lead.ErrLeadForbidden):
		return http.StatusForbidden, CodeForbidden, true
	}
	for target, status := range campaignStatuses {
		if errors.Is(err, target) {
			return status, campaign.ErrorCode(err), true
		}
	}
	for _, target := range templateErrors {
		if errors.Is(err, target) {
			return http.StatusUnprocessableEntity, CodeTemplateUnusable, true
		}
	}
	var unusable *uwc.InstanceUnusableError
	if errors.As(err, &unusable) {
		return http.StatusUnprocessableEntity, CodeInstanceUnusable, true
	}
	for _, target := range instanceErrors {
		if errors.Is(err, target) {
			return http.StatusUnprocessableEntity, CodeInstanceUnusable, true
		}
	}
	if code := selection.ErrorCode(err); code != "" {
		switch {
		case errors.Is(err, selection.ErrScopeDenied):
			return http.StatusForbidden, code, true
		case errors.Is(err, selection.ErrResolverUnavailable):
			return http.StatusServiceUnavailable, code, true
		}
		return http.StatusBadRequest, code, true
	}
	return 0, "", false
}

var selectionCodes = []string{
	"selection_unknown_mode", "selection_empty", "selection_ambiguous", "selection_too_many_ids", "selection_filter_required",
	"selection_limit_required", "selection_everyone_unconfirmed", "selection_fingerprint_mismatch", "selection_invalid_count",
	"selection_changed", "selection_mode_unsupported", "selection_unavailable", "selection_invalid_page", "selection_scope_denied",
	"selection_invalid_ids", "selection_unknown_sort", "selection_count_required", "invalid_filter",
}

func Codes() []string {
	codes := append([]string{}, audience.ErrorCodes()...)
	codes = append(codes, CodeBusy, CodeTemplateUnusable, CodeInstanceUnusable, CodeForbidden)
	for target := range campaignStatuses {
		codes = append(codes, campaign.ErrorCode(target))
	}
	return append(codes, selectionCodes...)
}
