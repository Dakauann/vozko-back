package httpx

import "vozko/domain/buttonaction"

type ButtonActionParams struct {
	Purpose      string `json:"purpose,omitempty" example:"receber ofertas da loja pelo WhatsApp"`
	DepartmentID string `json:"departmentId,omitempty" example:"5b0c1d2e-3f4a-4b5c-8d6e-7f8091a2b3c4"`
	UserID       string `json:"userId,omitempty"`
	WorkflowID   string `json:"workflowId,omitempty"`
	CallListID   string `json:"callListId,omitempty"`
	FieldKey     string `json:"fieldKey,omitempty" example:"interesse"`
	Option       string `json:"option,omitempty" example:"alto"`
	PipelineID   string `json:"pipelineId,omitempty"`
	StageID      string `json:"stageId,omitempty"`
}

type ButtonActionRequest struct {
	ButtonIndex  int                `json:"buttonIndex" example:"1"`
	ButtonText   string             `json:"buttonText" example:"SAIR"`
	Kind         string             `json:"kind" example:"opt_out" enums:"opt_out,consent_grant,hand_off,start_workflow,call_list,classify,create_deal"`
	Params       ButtonActionParams `json:"params"`
	Confirmation string             `json:"confirmation,omitempty" example:"Entendido. Você não receberá mais mensagens."`
}

func ButtonActionInputs(requests []ButtonActionRequest) []buttonaction.Input {
	out := make([]buttonaction.Input, 0, len(requests))
	for _, r := range requests {
		out = append(out, buttonaction.Input{ButtonIndex: r.ButtonIndex, ButtonText: r.ButtonText, Kind: buttonaction.Kind(r.Kind), Params: buttonaction.Params(r.Params), Confirmation: r.Confirmation})
	}
	return out
}
