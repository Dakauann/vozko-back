package lead

import (
	leaddomain "vozko/domain/lead"
	lead_usecase "vozko/usecases/lead"
)

type ConsentEvidenceRequest struct {
	Type    string `json:"type" enums:"message,form_submission,import_line,note,button_reply" example:"note"`
	Ref     string `json:"ref,omitempty" example:"wamid.HBgM"`
	Excerpt string `json:"excerpt,omitempty" maxLength:"280" example:"confirmou por telefone em 09/10"`
}

type RecordConsentRequest struct {
	Kind        string                  `json:"kind" enums:"grant,withdraw" example:"grant"`
	Source      string                  `json:"source" enums:"manual,conversation_request,api" example:"manual"`
	Purpose     string                  `json:"purpose,omitempty" maxLength:"200" example:"receber novidades pelo WhatsApp"`
	Evidence    *ConsentEvidenceRequest `json:"evidence,omitempty"`
	RequestedBy string                  `json:"requestedBy,omitempty" enums:"operator,lead_request" example:"lead_request"`
}

type RecordConsentResponse struct {
	Lead    LeadRecordResponse `json:"lead"`
	Changed bool               `json:"changed" example:"true"`
	EventID string             `json:"eventId,omitempty" example:"3f1c2b9e-8a7d-4c1e-9b2f-1d2e3f4a5b6c"`
}

func (r RecordConsentRequest) toInput() lead_usecase.ConsentInput {
	in := lead_usecase.ConsentInput{
		Kind: leaddomain.ConsentKind(r.Kind), Source: leaddomain.ConsentSource(r.Source),
		Purpose: r.Purpose, RequestedBy: leaddomain.OptOutSource(r.RequestedBy),
	}
	if r.Evidence != nil {
		in.Evidence = leaddomain.ConsentEvidence{Type: leaddomain.EvidenceType(r.Evidence.Type), Ref: r.Evidence.Ref, Excerpt: r.Evidence.Excerpt}
	}
	return in
}
