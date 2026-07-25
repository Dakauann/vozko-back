package workflow_usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"vozko/domain/cache"
	"vozko/domain/workflow"
	"vozko/pkg/webhookauth"
)

type WebhookRequest struct {
	Token   string
	Method  string
	RawBody []byte
	Header  func(name string) string
}

type WebhookResult struct {
	RunID          string
	Duplicate      bool
	AlreadyRunning bool
}

type webhookDedupGuard interface {
	IsDuplicate(key string) bool
}

var idempotencyHeaderCandidates = []string{"X-Idempotency-Key", "X-Webhook-Id", "X-Event-Id"}

type HandleWebhookTriggerUseCase interface {
	Execute(req WebhookRequest) (*WebhookResult, error)
}

type handleWebhookTrigger struct {
	webhookRepo  workflow.WorkflowWebhookRepository
	workflowRepo workflow.WorkflowRepository
	runRepo      workflow.WorkflowRunRepository
	entries      workflow.EntryOwnershipChecker
	resolver     workflow.EntryResolver
	launcher     RunLauncher
	dedup        webhookDedupGuard
	sharedState  cache.SharedState
}

func NewHandleWebhookTriggerUseCase(
	webhookRepo workflow.WorkflowWebhookRepository,
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
	entries workflow.EntryOwnershipChecker,
	entryResolver workflow.EntryResolver,
	launcher RunLauncher,
	dedup webhookDedupGuard,
	sharedState cache.SharedState,
) HandleWebhookTriggerUseCase {
	return &handleWebhookTrigger{
		webhookRepo:  webhookRepo,
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
		entries:      entries,
		resolver:     entryResolver,
		launcher:     launcher,
		dedup:        dedup,
		sharedState:  sharedState,
	}
}

func (uc *handleWebhookTrigger) Execute(req WebhookRequest) (*WebhookResult, error) {
	wh, err := uc.webhookRepo.FindByToken(req.Token)
	if err != nil {
		return nil, err
	}
	if wh == nil || !wh.Active {
		return nil, workflow.ErrWebhookNotFound
	}
	if !wh.AllowsMethod(req.Method) {
		return nil, workflow.ErrWebhookMethodNotAllowed
	}
	if err := verifyWebhookAuth(wh, req); err != nil {
		return nil, err
	}

	if uc.dedup != nil && uc.dedup.IsDuplicate(dedupKey(wh, req)) {
		return &WebhookResult{Duplicate: true}, nil
	}

	body := parseJSONBody(req.RawBody)
	entryID := stringField(body, "entry_id")
	entryType := stringField(body, "entry_type")

	switch {
	case entryID != "" && entryType != "":
		owns, err := uc.entries.OwnsEntry(wh.WorkspaceID, entryID, entryType)
		if err != nil {
			return nil, err
		}
		if !owns {
			return nil, workflow.ErrWebhookEntryForbidden
		}
	case stringField(body, "phone") != "":
		resolvedID, resolvedType, rerr := uc.resolver.ResolveByPhone(wh.WorkspaceID, stringField(body, "phone"))
		if rerr != nil {
			return nil, rerr
		}
		if resolvedID == "" {
			return nil, workflow.ErrWebhookEntryNotFound
		}
		entryID, entryType = resolvedID, resolvedType
	default:
		return nil, workflow.ErrWebhookEntryRequired
	}

	w, err := uc.workflowRepo.FindByID(wh.WorkflowID)
	if err != nil {
		return nil, err
	}
	if w == nil || w.Status != workflow.WorkflowStatusActive {
		return nil, workflow.ErrWorkflowNotActive
	}
	trigger := w.Graph.TriggerNodeByType(workflow.TriggerWebhook)
	if trigger == nil {
		return nil, workflow.ErrWebhookNoTriggerNode
	}

	existing, err := uc.runRepo.FindActiveByEntryAndTrigger(w.ID, entryID, trigger.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &WebhookResult{RunID: existing.ID, AlreadyRunning: true}, nil
	}

	if !tryAcquireWorkspaceSlot(uc.sharedState, wh.WorkspaceID) {
		return nil, workflow.ErrWorkspaceAtCapacity
	}

	event := workflow.TriggerEvent{
		WorkspaceID: wh.WorkspaceID,
		EntryID:     entryID,
		EntryType:   entryType,
		TriggerType: workflow.TriggerWebhook,
		Data:        map[string]interface{}{"webhook": buildWebhookVars(req, body)},
	}
	run := newTriggeredRun(w, trigger, event)
	if err := uc.runRepo.Create(run); err != nil {
		releaseWorkspaceSlot(uc.sharedState, wh.WorkspaceID)
		return nil, err
	}

	uc.launcher.Launch(run, w)
	return &WebhookResult{RunID: run.ID}, nil
}

func verifyWebhookAuth(wh *workflow.WorkflowWebhook, req WebhookRequest) error {
	switch wh.AuthMode {
	case workflow.WebhookAuthNone:
		return nil
	case workflow.WebhookAuthHeaderToken:
		if wh.Secret != "" && webhookauth.ConstantTimeEqual(req.Header(wh.HeaderName), wh.Secret) {
			return nil
		}
	case workflow.WebhookAuthHMAC:
		if webhookauth.VerifyPrefixedHMAC(wh.Secret, req.RawBody, req.Header(wh.HeaderName)) {
			return nil
		}
	}
	return workflow.ErrWebhookUnauthorized
}

func dedupKey(wh *workflow.WorkflowWebhook, req WebhookRequest) string {
	for _, h := range idempotencyHeaderCandidates {
		if v := strings.TrimSpace(req.Header(h)); v != "" {
			return "wfwh:" + wh.Token + ":" + v
		}
	}
	sum := sha256.Sum256(req.RawBody)
	return "wfwh:" + wh.Token + ":" + hex.EncodeToString(sum[:])
}

func parseJSONBody(raw []byte) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func buildWebhookVars(req WebhookRequest, body map[string]interface{}) map[string]interface{} {
	vars := map[string]interface{}{"method": req.Method}
	if body != nil {
		vars["body"] = body
	} else {
		vars["body"] = string(req.RawBody)
	}
	return vars
}
