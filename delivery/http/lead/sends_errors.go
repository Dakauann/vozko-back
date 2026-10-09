package lead

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/domain/unofficial_whatsapp"
	"vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	"vozko/domain/whatsapp_campaign"
	"vozko/domain/workspace/workspace_department"
	"vozko/usecases/campaignguard"
)

func writeSendError(w http.ResponseWriter, err error) {
	var refusal *campaign.BudgetRefusal
	if errors.As(err, &refusal) {
		response.WriteSuccess(w, http.StatusConflict, SendBudgetRefusalResponse{
			Error: true, Code: campaign.ErrorCode(err), Message: err.Error(), Fits: refusal.Fits,
		})
		return
	}
	writeActionError(w, err)
}

var sendStatuses = []struct {
	err    error
	status int
}{
	{campaign.ErrCreationScopeMissing, http.StatusForbidden},
	{campaign.ErrUnaffordable, http.StatusConflict},
	{campaign.ErrOverMonthlyCap, http.StatusConflict},
	{campaign.ErrNothingEligible, http.StatusConflict},
	{campaign.ErrAlreadyStarted, http.StatusConflict},
	{campaign.ErrSelectionSendLocked, http.StatusConflict},
	{campaign.ErrSendPreparing, http.StatusConflict},
	{template.ErrPricingUnavailable, http.StatusServiceUnavailable},
	{campaign.ErrSelectionOverCampaignCap, http.StatusRequestEntityTooLarge},
	{campaign.ErrSelectionTooLarge, http.StatusRequestEntityTooLarge},
	{campaign.ErrAutomationUnavailable, http.StatusServiceUnavailable},
	{campaign.ErrIdempotencyUnavailable, http.StatusServiceUnavailable},
	{campaign.ErrLeadTargetsUnavailable, http.StatusServiceUnavailable},
}

var sendCodes = []struct {
	err    error
	status int
	code   string
}{
	{whatsapp_campaign.ErrCampaignNotFound, http.StatusNotFound, "send_campaign_not_found"},
	{unofficial_whatsapp_campaign.ErrCampaignNotFound, http.StatusNotFound, "send_campaign_not_found"},
	{campaign.ErrNothingToSend, http.StatusConflict, "send_nothing_eligible"},
	{campaign.ErrAlreadySent, http.StatusConflict, "send_already_started"},
	{whatsapp_campaign.ErrCampaignNoSubscription, http.StatusPaymentRequired, "NO_ACTIVE_SUBSCRIPTION"},
	{whatsapp_campaign.ErrCampaignTemplateNotFound, http.StatusUnprocessableEntity, "send_template_unavailable"},
	{whatsapp_campaign.ErrCampaignTemplateNotApproved, http.StatusUnprocessableEntity, "send_template_unavailable"},
	{template.ErrTemplateNotSendable, http.StatusUnprocessableEntity, "send_template_unavailable"},
	{whatsapp_campaign.ErrCampaignTemplatePhoneMismatch, http.StatusUnprocessableEntity, "send_template_phone_mismatch"},
	{whatsapp_campaign.ErrCampaignBusinessPhoneNotFound, http.StatusUnprocessableEntity, "send_phone_unavailable"},
	{whatsapp_campaign.ErrCampaignBusinessPhoneNoAccess, http.StatusUnprocessableEntity, "send_phone_unavailable"},
	{template.ErrBillingNotConfigured, http.StatusServiceUnavailable, "send_pricing_unavailable"},
	{unofficial_whatsapp.ErrInstanceNotFound, http.StatusUnprocessableEntity, "send_instance_unavailable"},
	{unofficial_whatsapp.ErrInstanceOutsideDepartment, http.StatusUnprocessableEntity, "send_instance_unavailable"},
	{unofficial_whatsapp.ErrInstanceNotConnected, http.StatusUnprocessableEntity, "send_instance_unavailable"},
	{unofficial_whatsapp.ErrRestrictedByWA, http.StatusUnprocessableEntity, "send_instance_unavailable"},
	{campaignguard.ErrUnavailable, http.StatusServiceUnavailable, "lead_sends_unavailable"},
	{workspace_department.ErrDepartmentAccessDenied, http.StatusForbidden, "send_department_forbidden"},
	{workspace_department.ErrDepartmentNotFound, http.StatusForbidden, "send_department_forbidden"},
	{workspace_department.ErrDepartmentRequired, http.StatusBadRequest, "send_department_required"},
	{unofficial_whatsapp_campaign.ErrMessageKindInvalid, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMessageBodyRequired, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMessageBodyEmpty, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMessageBodyTooLong, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMessageMediaRequired, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMessageVariantMismatch, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMenuOptionsRequired, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMenuOptionsTooMany, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMenuOptionLabelTooLong, http.StatusBadRequest, "send_message_invalid"},
	{unofficial_whatsapp_campaign.ErrMenuOptionIDRequired, http.StatusBadRequest, "send_message_invalid"},
}

func sendRefusal(err error) (int, string, bool) {
	var unusable *unofficial_whatsapp_campaign.InstanceUnusableError
	if errors.As(err, &unusable) {
		return http.StatusUnprocessableEntity, "send_instance_unavailable", true
	}
	var notReady *whatsapp_campaign.TemplateNotReadyError
	if errors.As(err, &notReady) {
		return http.StatusUnprocessableEntity, "send_template_unavailable", true
	}
	if code := campaign.ErrorCode(err); code != "" {
		for _, known := range sendStatuses {
			if errors.Is(err, known.err) {
				return known.status, code, true
			}
		}
		return http.StatusBadRequest, code, true
	}
	for _, known := range sendCodes {
		if errors.Is(err, known.err) {
			return known.status, known.code, true
		}
	}
	return 0, "", false
}
