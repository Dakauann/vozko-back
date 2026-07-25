package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func newConfigUC() (WorkflowWebhookUseCase, *fakeWebhookRepo, *MockWorkflowRepository) {
	webhooks := newFakeWebhookRepo()
	wfRepo := NewMockWorkflowRepository()
	wfRepo.workflows["wf1"] = webhookWorkflow()
	return NewWorkflowWebhookUseCase(webhooks, wfRepo), webhooks, wfRepo
}

func TestWebhookConfig_CreateDefaults(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	wh, err := uc.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if wh.AuthMode != workflow.WebhookAuthHeaderToken {
		t.Fatalf("expected default header_token, got %s", wh.AuthMode)
	}
	if wh.Token == "" || wh.Secret == "" {
		t.Fatal("expected token and secret generated")
	}
	if wh.HeaderName != workflow.DefaultWebhookTokenHeader {
		t.Fatalf("expected default header, got %s", wh.HeaderName)
	}
	if wh.Method != "POST" || !wh.Active {
		t.Fatalf("expected active POST default, got %s active=%v", wh.Method, wh.Active)
	}
	if len(webhooks.created) != 1 {
		t.Fatal("expected webhook persisted")
	}
}

func TestWebhookConfig_CreateCustomAndNone(t *testing.T) {
	uc, _, _ := newConfigUC()
	wh, err := uc.Create(WebhookConfigInput{
		WorkspaceID: "ws1", WorkflowID: "wf1",
		AuthMode: workflow.WebhookAuthNone, Method: "PUT", HeaderName: "X-Custom",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if wh.Secret != "" {
		t.Fatal("auth none must not carry a secret")
	}
	if wh.Method != "PUT" {
		t.Fatalf("expected custom method, got %s", wh.Method)
	}
}

func TestWebhookConfig_CreateAlreadyExists(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	webhooks.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone})
	if _, err := uc.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); !errors.Is(err, workflow.ErrWebhookAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookConfig_CreateInvalidMode(t *testing.T) {
	uc, _, _ := newConfigUC()
	if _, err := uc.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1", AuthMode: "bogus"}); !errors.Is(err, workflow.ErrWebhookInvalidAuthMode) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookConfig_NotOwned(t *testing.T) {
	uc, _, _ := newConfigUC()
	if _, err := uc.Create(WebhookConfigInput{WorkspaceID: "other", WorkflowID: "wf1"}); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("create cross-workspace: got %v", err)
	}
	if _, err := uc.Get("other", "wf1"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("get cross-workspace: got %v", err)
	}
	if err := uc.Delete("other", "wf1"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("delete cross-workspace: got %v", err)
	}
}

func TestWebhookConfig_OwnershipLookupError(t *testing.T) {
	uc, _, wfRepo := newConfigUC()
	wfRepo.FindErr = errors.New("db down")
	if _, err := uc.Get("ws1", "wf1"); err == nil {
		t.Fatal("expected ownership lookup error to propagate")
	}
}

func TestWebhookConfig_Get(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	if wh, err := uc.Get("ws1", "wf1"); err != nil || wh != nil {
		t.Fatalf("expected nil when unconfigured, got %v err=%v", wh, err)
	}
	webhooks.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone})
	wh, err := uc.Get("ws1", "wf1")
	if err != nil || wh == nil || wh.ID != "x" {
		t.Fatalf("expected configured webhook, got %v err=%v", wh, err)
	}
}

func TestWebhookConfig_Update(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	webhooks.put(&workflow.WorkflowWebhook{
		ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t",
		AuthMode: workflow.WebhookAuthNone, Method: "POST", Active: true,
	})

	active := false
	wh, err := uc.Update(WebhookConfigInput{
		WorkspaceID: "ws1", WorkflowID: "wf1",
		AuthMode: workflow.WebhookAuthHMAC, Active: &active,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if wh.AuthMode != workflow.WebhookAuthHMAC || wh.Secret == "" {
		t.Fatal("switching none->hmac must mint a secret")
	}
	if wh.HeaderName != workflow.DefaultWebhookSignatureHeader {
		t.Fatalf("expected hmac default header, got %s", wh.HeaderName)
	}
	if wh.Active {
		t.Fatal("expected active=false applied")
	}

	back, err := uc.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1", AuthMode: workflow.WebhookAuthNone})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if back.Secret != "" {
		t.Fatal("switching to none must clear the secret")
	}
}

func TestWebhookConfig_UpdateNotFound(t *testing.T) {
	uc, _, _ := newConfigUC()
	if _, err := uc.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); !errors.Is(err, workflow.ErrWebhookNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookConfig_UpdateInvalidMode(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	webhooks.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone})
	if _, err := uc.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1", AuthMode: "bogus"}); !errors.Is(err, workflow.ErrWebhookInvalidAuthMode) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookConfig_Rotate(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	webhooks.put(&workflow.WorkflowWebhook{
		ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "old", Secret: "olds",
		AuthMode: workflow.WebhookAuthHMAC, HeaderName: "X-Sig", Method: "POST", Active: true,
	})
	wh, err := uc.Rotate("ws1", "wf1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if wh.Token == "old" || wh.Secret == "olds" {
		t.Fatal("rotate must change token and secret")
	}
}

func TestWebhookConfig_RotateNoneKeepsEmptySecret(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	webhooks.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "old", AuthMode: workflow.WebhookAuthNone, Active: true})
	wh, err := uc.Rotate("ws1", "wf1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if wh.Token == "old" {
		t.Fatal("rotate must change token")
	}
	if wh.Secret != "" {
		t.Fatal("none mode must keep an empty secret on rotate")
	}
}

func TestWebhookConfig_RotateNotFound(t *testing.T) {
	uc, _, _ := newConfigUC()
	if _, err := uc.Rotate("ws1", "wf1"); !errors.Is(err, workflow.ErrWebhookNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookConfig_RepoErrorsPropagate(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	webhooks.findErr = errors.New("find failed")
	if _, err := uc.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); err == nil {
		t.Fatal("create must surface FindByWorkflowID error")
	}

	uc2, webhooks2, _ := newConfigUC()
	webhooks2.createErr = errors.New("insert failed")
	if _, err := uc2.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); err == nil {
		t.Fatal("create must surface repo Create error")
	}

	uc3, webhooks3, _ := newConfigUC()
	webhooks3.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone})
	webhooks3.updateErr = errors.New("update failed")
	if _, err := uc3.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); err == nil {
		t.Fatal("update must surface repo Update error")
	}
	if _, err := uc3.Rotate("ws1", "wf1"); err == nil {
		t.Fatal("rotate must surface repo Update error")
	}
}

func TestWebhookConfig_TokenGenErrors(t *testing.T) {
	orig := randomToken
	defer func() { randomToken = orig }()

	randomToken = func(int) (string, error) { return "", errors.New("no entropy") }

	uc, _, _ := newConfigUC()
	if _, err := uc.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); err == nil {
		t.Fatal("create must surface token generation error")
	}

	uc2, webhooks2, _ := newConfigUC()
	webhooks2.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone})
	if _, err := uc2.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1", AuthMode: workflow.WebhookAuthHMAC}); err == nil {
		t.Fatal("update secret minting must surface generation error")
	}

	uc3, webhooks3, _ := newConfigUC()
	webhooks3.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthHMAC, HeaderName: "X-Sig", Secret: "s"})
	if _, err := uc3.Rotate("ws1", "wf1"); err == nil {
		t.Fatal("rotate token must surface generation error")
	}

	// Token succeeds but the secret fails: exercises the secret-generation branch
	// of Create/Rotate specifically.
	randomToken = func(n int) (string, error) {
		if n == webhookSecretBytes {
			return "", errors.New("no entropy")
		}
		return "tok", nil
	}
	uc4, _, _ := newConfigUC()
	if _, err := uc4.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); err == nil {
		t.Fatal("create must surface secret generation error")
	}
	uc5, webhooks5, _ := newConfigUC()
	webhooks5.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthHMAC, HeaderName: "X-Sig", Secret: "s"})
	if _, err := uc5.Rotate("ws1", "wf1"); err == nil {
		t.Fatal("rotate must surface secret generation error")
	}
}

func TestWebhookConfig_UpdateAndRotateBranches(t *testing.T) {
	// Update: cross-workspace ownership rejection.
	uc, webhooks, _ := newConfigUC()
	webhooks.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone})
	if _, err := uc.Update(WebhookConfigInput{WorkspaceID: "other", WorkflowID: "wf1"}); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("update cross-workspace: got %v", err)
	}
	if _, err := uc.Rotate("other", "wf1"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("rotate cross-workspace: got %v", err)
	}

	// Update / Rotate: webhook lookup error.
	uc2, webhooks2, _ := newConfigUC()
	webhooks2.findErr = errors.New("lookup failed")
	if _, err := uc2.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); err == nil {
		t.Fatal("update must surface webhook lookup error")
	}
	if _, err := uc2.Rotate("ws1", "wf1"); err == nil {
		t.Fatal("rotate must surface webhook lookup error")
	}

	// Update: custom method branch.
	uc3, webhooks3, _ := newConfigUC()
	webhooks3.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone, Method: "POST"})
	wh, err := uc3.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1", Method: "PUT"})
	if err != nil || wh.Method != "PUT" {
		t.Fatalf("expected method updated to PUT, got %s err=%v", wh.Method, err)
	}

	// Update: a stored webhook made invalid (empty token) fails Validate.
	uc4, webhooks4, _ := newConfigUC()
	webhooks4.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "", AuthMode: workflow.WebhookAuthNone})
	if _, err := uc4.Update(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); !errors.Is(err, workflow.ErrWebhookTokenRequired) {
		t.Fatalf("expected validate failure, got %v", err)
	}
}

func TestWebhookConfig_CreateValidateFailure(t *testing.T) {
	orig := randomToken
	defer func() { randomToken = orig }()
	randomToken = func(int) (string, error) { return "", nil } // empty token, no error

	uc, _, _ := newConfigUC()
	if _, err := uc.Create(WebhookConfigInput{WorkspaceID: "ws1", WorkflowID: "wf1"}); !errors.Is(err, workflow.ErrWebhookTokenRequired) {
		t.Fatalf("expected validate failure on empty token, got %v", err)
	}
}

func TestWebhookConfig_Delete(t *testing.T) {
	uc, webhooks, _ := newConfigUC()
	webhooks.put(&workflow.WorkflowWebhook{ID: "x", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "t", AuthMode: workflow.WebhookAuthNone})
	if err := uc.Delete("ws1", "wf1"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(webhooks.deleted) != 1 || webhooks.deleted[0] != "wf1" {
		t.Fatalf("expected delete by workflow id, got %v", webhooks.deleted)
	}
}
