package httpx

import (
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/workflow"
)

var WorkflowAccessRefusals = []response.ErrorRefusal{
	{Err: workflow.ErrWorkflowNotFound, Refusal: response.Refusal{Status: http.StatusNotFound, Code: "workflow_not_found", Message: "Workflow não encontrado"}},
	{Err: workflow.ErrWorkflowAccessDenied, Refusal: response.Refusal{Status: http.StatusForbidden, Code: "workflow_access_denied", Message: "Você não tem acesso a este workflow"}},
}

var RefusalWorkflowUnreadable = response.Refusal{Status: http.StatusInternalServerError, Code: "workflow_unreadable", Message: "Erro ao ler o workflow"}

type WorkflowAccess interface {
	Get(scope workflow.Scope, workflowID string) (*workflow.Workflow, error)
}

func RequireWorkflowAccess(w http.ResponseWriter, r *http.Request, workflows WorkflowAccess, workflowID string) bool {
	if workflows == nil {
		response.WriteRefusal(w, RefusalWorkflowUnreadable)
		return false
	}
	if _, err := workflows.Get(WorkflowScope(r), workflowID); err != nil {
		WriteWorkflowAccessError(w, err)
		return false
	}
	return true
}

func WriteWorkflowAccessError(w http.ResponseWriter, err error) {
	if !response.WriteErrorRefusal(w, err, WorkflowAccessRefusals) {
		response.WriteRefusal(w, RefusalWorkflowUnreadable)
	}
}
