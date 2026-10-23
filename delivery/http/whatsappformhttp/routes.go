package whatsappformhttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	workspace_domain "vozko/domain/workspace"
)

const (
	formsPath    = "/whatsapp/forms"
	formPath     = formsPath + "/{formId:" + httpx.UUIDPattern + "}"
	responsePath = formsPath + "/responses/{responseId:" + httpx.UUIDPattern + "}"
	sendPath     = "/conversations/whatsapp/{entryId:" + httpx.UUIDPattern + "}/forms"
)

func RegisterRoutes(protected *mux.Router, h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	forms, answers := workspace_domain.ResourceWhatsAppForms, workspace_domain.ResourceWhatsAppFormResponses
	read, create, update := workspace_domain.ActionRead, workspace_domain.ActionCreate, workspace_domain.ActionUpdate
	configure, send, del := workspace_domain.ActionConfigure, workspace_domain.ActionSend, workspace_domain.ActionDelete

	protected.HandleFunc(formsPath+"/options", ac(forms, read, h.Options)).Methods(http.MethodGet)
	protected.HandleFunc(formsPath+"/check", ac(forms, read, h.Check)).Methods(http.MethodPost)
	protected.HandleFunc(formsPath+"/sendable", ac(forms, read, h.Sendable)).Methods(http.MethodGet)
	protected.HandleFunc(formsPath+"/publish-targets", ac(forms, configure, h.PublishTargets)).Methods(http.MethodGet)
	protected.HandleFunc(formsPath, ac(forms, read, h.List)).Methods(http.MethodGet)
	protected.HandleFunc(formsPath, ac(forms, create, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc(responsePath, ac(answers, read, h.Response)).Methods(http.MethodGet)
	protected.HandleFunc(responsePath, ac(answers, del, h.DeleteResponse)).Methods(http.MethodDelete)
	protected.HandleFunc(formPath, ac(forms, read, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc(formPath, ac(forms, update, h.Rename)).Methods(http.MethodPatch)
	protected.HandleFunc(formPath+"/draft", ac(forms, update, h.SaveDraft)).Methods(http.MethodPut)
	protected.HandleFunc(formPath+"/compiled", ac(forms, read, h.Compiled)).Methods(http.MethodGet)
	protected.HandleFunc(formPath+"/versions", ac(forms, read, h.Versions)).Methods(http.MethodGet)
	protected.HandleFunc(formPath+"/duplicate", ac(forms, create, h.Duplicate)).Methods(http.MethodPost)
	protected.HandleFunc(formPath+"/archive", ac(forms, del, h.Archive)).Methods(http.MethodPost)
	protected.HandleFunc(formPath+"/publish", ac(forms, configure, h.Publish)).Methods(http.MethodPost)
	protected.HandleFunc(formPath+"/publications/{metaWabaId}/deprecate", ac(forms, configure, h.Deprecate)).Methods(http.MethodPost)
	protected.HandleFunc(formPath+"/preview", ac(forms, update, h.Preview)).Methods(http.MethodPost)
	protected.HandleFunc(formPath+"/sends", ac(forms, read, h.Sends)).Methods(http.MethodGet)
	protected.HandleFunc(formPath+"/automations", ac(forms, read, h.Automations)).Methods(http.MethodGet)
	protected.HandleFunc(formPath+"/automations", ac(forms, read, h.ScaffoldAutomation)).Methods(http.MethodPost)
	protected.HandleFunc(formPath+"/responses", ac(answers, read, h.Responses)).Methods(http.MethodGet)
	protected.HandleFunc(formPath+"/stats", ac(answers, read, h.Stats)).Methods(http.MethodGet)
	protected.HandleFunc(formPath+"/fields/{fieldKey}/distribution", ac(answers, read, h.Distribution)).Methods(http.MethodGet)
	protected.HandleFunc(sendPath, ac(forms, send, h.Send)).Methods(http.MethodPost)
}
