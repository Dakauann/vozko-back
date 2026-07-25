package workflow_usecase

import (
	"vozko/domain/workflow"
	"vozko/pkg/webhookauth"
)

const (
	webhookTokenBytes  = 24
	webhookSecretBytes = 32
)

var randomToken = webhookauth.RandomToken

type WebhookConfigInput struct {
	WorkspaceID string
	WorkflowID  string
	AuthMode    workflow.WebhookAuthMode
	HeaderName  string
	Method      string
	Active      *bool
}

type WorkflowWebhookUseCase interface {
	Get(workspaceID, workflowID string) (*workflow.WorkflowWebhook, error)
	Create(input WebhookConfigInput) (*workflow.WorkflowWebhook, error)
	Update(input WebhookConfigInput) (*workflow.WorkflowWebhook, error)
	Rotate(workspaceID, workflowID string) (*workflow.WorkflowWebhook, error)
	Delete(workspaceID, workflowID string) error
}

type workflowWebhookUseCase struct {
	webhookRepo  workflow.WorkflowWebhookRepository
	workflowRepo workflow.WorkflowRepository
}

func NewWorkflowWebhookUseCase(
	webhookRepo workflow.WorkflowWebhookRepository,
	workflowRepo workflow.WorkflowRepository,
) WorkflowWebhookUseCase {
	return &workflowWebhookUseCase{webhookRepo: webhookRepo, workflowRepo: workflowRepo}
}

func (uc *workflowWebhookUseCase) Get(workspaceID, workflowID string) (*workflow.WorkflowWebhook, error) {
	if err := uc.assertOwned(workspaceID, workflowID); err != nil {
		return nil, err
	}
	return uc.webhookRepo.FindByWorkflowID(workflowID)
}

func (uc *workflowWebhookUseCase) Create(input WebhookConfigInput) (*workflow.WorkflowWebhook, error) {
	if err := uc.assertOwned(input.WorkspaceID, input.WorkflowID); err != nil {
		return nil, err
	}
	existing, err := uc.webhookRepo.FindByWorkflowID(input.WorkflowID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, workflow.ErrWebhookAlreadyExists
	}

	mode := input.AuthMode
	if mode == "" {
		mode = workflow.WebhookAuthHeaderToken
	}
	if !mode.Valid() {
		return nil, workflow.ErrWebhookInvalidAuthMode
	}

	token, err := randomToken(webhookTokenBytes)
	if err != nil {
		return nil, err
	}
	secret, err := newSecretForMode(mode)
	if err != nil {
		return nil, err
	}

	wh := &workflow.WorkflowWebhook{
		WorkflowID:  input.WorkflowID,
		WorkspaceID: input.WorkspaceID,
		Token:       token,
		AuthMode:    mode,
		Secret:      secret,
		HeaderName:  resolveHeaderName(mode, input.HeaderName),
		Method:      resolveMethod(input.Method),
		Active:      true,
	}
	if err := wh.Validate(); err != nil {
		return nil, err
	}
	if err := uc.webhookRepo.Create(wh); err != nil {
		return nil, err
	}
	return wh, nil
}

func (uc *workflowWebhookUseCase) Update(input WebhookConfigInput) (*workflow.WorkflowWebhook, error) {
	if err := uc.assertOwned(input.WorkspaceID, input.WorkflowID); err != nil {
		return nil, err
	}
	wh, err := uc.webhookRepo.FindByWorkflowID(input.WorkflowID)
	if err != nil {
		return nil, err
	}
	if wh == nil {
		return nil, workflow.ErrWebhookNotFound
	}

	mode := input.AuthMode
	if mode == "" {
		mode = wh.AuthMode
	}
	if !mode.Valid() {
		return nil, workflow.ErrWebhookInvalidAuthMode
	}

	switch {
	case mode == workflow.WebhookAuthNone:
		wh.Secret = ""
	case wh.Secret == "":
		secret, err := randomToken(webhookSecretBytes)
		if err != nil {
			return nil, err
		}
		wh.Secret = secret
	}

	wh.AuthMode = mode
	wh.HeaderName = resolveHeaderName(mode, input.HeaderName)
	if input.Method != "" {
		wh.Method = resolveMethod(input.Method)
	}
	if input.Active != nil {
		wh.Active = *input.Active
	}

	if err := wh.Validate(); err != nil {
		return nil, err
	}
	if err := uc.webhookRepo.Update(wh); err != nil {
		return nil, err
	}
	return wh, nil
}

func (uc *workflowWebhookUseCase) Rotate(workspaceID, workflowID string) (*workflow.WorkflowWebhook, error) {
	if err := uc.assertOwned(workspaceID, workflowID); err != nil {
		return nil, err
	}
	wh, err := uc.webhookRepo.FindByWorkflowID(workflowID)
	if err != nil {
		return nil, err
	}
	if wh == nil {
		return nil, workflow.ErrWebhookNotFound
	}

	token, err := randomToken(webhookTokenBytes)
	if err != nil {
		return nil, err
	}
	wh.Token = token
	if wh.AuthMode != workflow.WebhookAuthNone {
		secret, err := randomToken(webhookSecretBytes)
		if err != nil {
			return nil, err
		}
		wh.Secret = secret
	}
	if err := uc.webhookRepo.Update(wh); err != nil {
		return nil, err
	}
	return wh, nil
}

func (uc *workflowWebhookUseCase) Delete(workspaceID, workflowID string) error {
	if err := uc.assertOwned(workspaceID, workflowID); err != nil {
		return err
	}
	return uc.webhookRepo.Delete(workflowID)
}

func (uc *workflowWebhookUseCase) assertOwned(workspaceID, workflowID string) error {
	w, err := uc.workflowRepo.FindByID(workflowID)
	if err != nil {
		return err
	}
	if w == nil || w.WorkspaceID != workspaceID {
		return workflow.ErrWorkflowNotFound
	}
	return nil
}

func newSecretForMode(mode workflow.WebhookAuthMode) (string, error) {
	if mode == workflow.WebhookAuthNone {
		return "", nil
	}
	return randomToken(webhookSecretBytes)
}

func resolveHeaderName(mode workflow.WebhookAuthMode, provided string) string {
	if provided != "" {
		return provided
	}
	switch mode {
	case workflow.WebhookAuthHeaderToken:
		return workflow.DefaultWebhookTokenHeader
	case workflow.WebhookAuthHMAC:
		return workflow.DefaultWebhookSignatureHeader
	default:
		return ""
	}
}

func resolveMethod(provided string) string {
	if provided == "" {
		return workflow.DefaultWebhookMethod
	}
	return provided
}
