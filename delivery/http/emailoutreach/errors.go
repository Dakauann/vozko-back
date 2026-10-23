package emailoutreach

import (
	"net/http"

	"vozko/delivery/http/emailrefusal"
	"vozko/delivery/http/response"
	"vozko/usecases/emailoutreach"
)

var refusalSendFailed = emailrefusal.SendFailed

var outreachRefusals = append(append(append([]response.Refusal{}, emailrefusal.Transactional...), emailrefusal.Hygiene...),
	response.Refusal{Status: http.StatusForbidden, Code: emailoutreach.CodeForbidden, Message: "Este remetente pertence a um departamento fora do seu acesso."},
)

var refusalByCode = emailrefusal.Index(outreachRefusals)

func RefusalCodes() []string {
	return response.RefusalCodes(outreachRefusals, []response.Refusal{refusalSendFailed})
}

func writeStartRefusal(w http.ResponseWriter, workspaceID string, err error) {
	emailrefusal.Write(w, "[email-outreach]", workspaceID, err, refusalByCode)
}
