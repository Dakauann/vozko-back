package audience_usecase

import (
	"context"
	"log"
	"strings"

	ca "vozko/domain/audience"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
)

type decisionRoleInferrer struct {
	llm   ca.RoleInferrer
	model decision.Model
	live  LiveWorkspaces
}

func NewDecisionRoleInferrer(llm ca.RoleInferrer, model decision.Model, live LiveWorkspaces) ca.RoleInferrer {
	if model == nil || live == nil {
		return llm
	}
	return &decisionRoleInferrer{llm: llm, model: model, live: live}
}

func (r *decisionRoleInferrer) InferRole(ctx context.Context, req ca.RoleInferRequest) (*ca.RoleInferResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if !r.live.Acts(ctx, req.WorkspaceID) {
		return r.llm.InferRole(ctx, req)
	}
	result, err := r.decide(ctx, req)
	if err != nil {
		log.Printf("[audience-decisions] workspace %s falls back to the llm role inference: %v", req.WorkspaceID, err)
		return r.llm.InferRole(ctx, req)
	}
	return result, nil
}

func (r *decisionRoleInferrer) decide(ctx context.Context, req ca.RoleInferRequest) (*ca.RoleInferResult, error) {
	state := map[string]any{"comentarios": req.Comments}
	if instructions := strings.TrimSpace(req.Instructions); instructions != "" {
		state["contexto_da_conta"] = truncateRunes(instructions, ca.MaxInstructionsRunes)
	}
	result, err := r.model.Decide(ctx, decision.Request{
		WorkspaceID: req.WorkspaceID,
		Purpose:     ld.PurposeAuthorRole,
		State:       state,
		Questions:   ca.AuthorRoleQuestions(),
	})
	if err != nil {
		return nil, err
	}
	role, confidence, err := ca.AuthorRoleFrom(result)
	if err != nil {
		return nil, err
	}
	return &ca.RoleInferResult{Role: role, Confidence: string(confidence), Model: result.Model}, nil
}
