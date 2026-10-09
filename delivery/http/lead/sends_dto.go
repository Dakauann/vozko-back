package lead

import (
	"vozko/delivery/http/unofficial_whatsapp"
	"vozko/domain/campaign"
	"vozko/domain/leadaction"
)

type LeadSendRequest struct {
	Channel     string   `json:"channel" example:"official"`
	CampaignIDs []string `json:"campaignIds"`
	FirstN      int      `json:"firstN,omitempty" example:"0"`
}

type LeadSendCancelResponse struct {
	Cancelled bool `json:"cancelled" example:"true"`
}

type SendBudgetRefusalResponse struct {
	Error   bool   `json:"error" example:"true"`
	Code    string `json:"code" example:"unaffordable"`
	Message string `json:"message"`
	Fits    int    `json:"fits" example:"1040"`
}

type LeadSendParams struct {
	Name            string                              `json:"name" example:"Matrículas 2027"`
	DepartmentID    string                              `json:"departmentId,omitempty"`
	Split           bool                                `json:"split,omitempty"`
	BusinessPhoneID string                              `json:"businessPhoneId,omitempty"`
	TemplateID      string                              `json:"templateId,omitempty"`
	InstanceID      string                              `json:"instanceId,omitempty"`
	Message         *unofficial_whatsapp.MessageSpecDTO `json:"message,omitempty"`
	SendDelayMinMS  int                                 `json:"sendDelayMinMs,omitempty"`
	SendDelayMaxMS  int                                 `json:"sendDelayMaxMs,omitempty"`
	DailyCap        int                                 `json:"dailyCap,omitempty"`
	Bindings        []campaign.VariableBinding          `json:"bindings,omitempty"`
}

func (p *LeadSendParams) toDomain() *leadaction.SendParams {
	if p == nil {
		return nil
	}
	send := &leadaction.SendParams{
		Name: p.Name, DepartmentID: p.DepartmentID, Split: p.Split, BusinessPhoneID: p.BusinessPhoneID, TemplateID: p.TemplateID,
		InstanceID: p.InstanceID, SendDelayMinMS: p.SendDelayMinMS, SendDelayMaxMS: p.SendDelayMaxMS, DailyCap: p.DailyCap, Bindings: p.Bindings,
	}
	if p.Message != nil {
		message := p.Message.ToDomain()
		send.Message = &message
	}
	return send
}
