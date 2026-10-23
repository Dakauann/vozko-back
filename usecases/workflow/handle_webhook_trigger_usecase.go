package workflow_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/metrics"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/pkg/webhookanswer"
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
	Ignored        bool
}

type WebhookLeads interface {
	EnsureLead(ctx context.Context, workspaceID, phone, name string) (*lead_domain.Lead, error)
	FindByEmail(workspaceID, email string) (*lead_domain.Lead, error)
}

type WebhookLeadStart struct {
	Leads   WebhookLeads
	Starter leadStarter
}

type webhookDedupGuard interface {
	IsDuplicate(key string) bool
	Forget(key string) error
}

var idempotencyHeaderCandidates = []string{"X-Idempotency-Key", "X-Webhook-Id", "X-Event-Id"}

type HandleWebhookTriggerUseCase interface {
	Execute(req WebhookRequest) (*WebhookResult, error)
}

type handleWebhookTrigger struct {
	webhookRepo  workflow.WorkflowWebhookRepository
	workflowRepo workflow.WorkflowRepository
	entries      workflow.EntryOwnershipChecker
	resolver     workflow.EntryResolver
	starter      runStarter
	dedup        webhookDedupGuard
	deliveries   workflow.WebhookDeliveryRepository
	metrics      metrics.WorkflowMetricsRecorder
	leadStart    WebhookLeadStart
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
	deliveries workflow.WebhookDeliveryRepository,
	recorder metrics.WorkflowMetricsRecorder,
	leadStart WebhookLeadStart,
) HandleWebhookTriggerUseCase {
	if deliveries == nil || recorder == nil {
		panic("workflow: the webhook trigger needs the delivery log and the metrics recorder")
	}
	if leadStart.Leads == nil || leadStart.Starter == nil {
		panic("workflow: the webhook trigger needs the leads and the lead run starter")
	}
	return &handleWebhookTrigger{
		webhookRepo:  webhookRepo,
		workflowRepo: workflowRepo,
		entries:      entries,
		resolver:     entryResolver,
		starter:      runStarter{runs: runRepo, launcher: launcher, sharedState: sharedState, workflows: workflowRepo},
		dedup:        dedup,
		deliveries:   deliveries,
		metrics:      recorder,
		leadStart:    leadStart,
	}
}

func (uc *handleWebhookTrigger) Execute(req WebhookRequest) (*WebhookResult, error) {
	received := time.Now().UTC()
	wh, err := uc.webhookRepo.FindByToken(req.Token)
	if err == nil && wh == nil {
		err = workflow.ErrWebhookNotFound
	}
	if err != nil {
		uc.metrics.IncWorkflowWebhookDelivery(string(workflow.WebhookOutcomeRejected), workflow.WebhookRejectionReason(err))
		return nil, err
	}

	delivery := &workflow.WebhookDelivery{
		ID:          uuid.New().String(),
		WebhookID:   wh.ID,
		WorkflowID:  wh.WorkflowID,
		WorkspaceID: wh.WorkspaceID,
		ReceivedAt:  received,
	}
	result, err := uc.deliver(wh, req, delivery)
	uc.record(delivery, result, err, received)
	return result, err
}

func (uc *handleWebhookTrigger) deliver(wh *workflow.WorkflowWebhook, req WebhookRequest, delivery *workflow.WebhookDelivery) (result *WebhookResult, err error) {
	if !wh.Active {
		return nil, workflow.ErrWebhookInactive
	}
	if !wh.AllowsMethod(req.Method) {
		return nil, workflow.ErrWebhookMethodNotAllowed
	}
	if err := verifyWebhookAuth(wh, req); err != nil {
		return nil, err
	}

	body := parseJSONBody(req.RawBody)
	delivery.PayloadKeys = workflow.PayloadKeys(body)

	if uc.dedup == nil {
		return uc.accept(wh, req, body, delivery)
	}
	key := dedupKey(wh, req)
	if uc.dedup.IsDuplicate(key) {
		return &WebhookResult{Duplicate: true}, nil
	}
	defer func() {
		if err == nil {
			return
		}
		if forgetErr := uc.dedup.Forget(key); forgetErr != nil {
			slog.Warn("workflow.webhook.dedup_forget_failed", slog.String("workspace_id", wh.WorkspaceID), slog.String("workflow_id", wh.WorkflowID), slog.String("error", forgetErr.Error()))
		}
	}()
	return uc.accept(wh, req, body, delivery)
}

func (uc *handleWebhookTrigger) accept(wh *workflow.WorkflowWebhook, req WebhookRequest, body map[string]interface{}, delivery *workflow.WebhookDelivery) (*WebhookResult, error) {
	w, trigger, err := uc.webhookWorkflow(wh)
	if err != nil {
		return nil, err
	}
	settings, err := workflow.ReadWebhookTrigger(trigger.Config)
	if err != nil {
		return nil, err
	}
	if !settings.Accepts(stringField(body, "event")) {
		return &WebhookResult{Ignored: true}, nil
	}
	if settings.Start == workflow.WebhookStartLead {
		return uc.startOnLead(w, wh, req, body, delivery)
	}

	entryID := stringField(body, "entry_id")
	entryType := stringField(body, "entry_type")
	switch {
	case entryID != "" && entryType != "":
		delivery.EntryResolution = workflow.WebhookEntryByID
		owns, err := uc.entries.OwnsEntry(wh.WorkspaceID, entryID, entryType)
		if err != nil {
			return nil, err
		}
		if !owns {
			return nil, workflow.ErrWebhookEntryForbidden
		}
	case stringField(body, "phone") != "":
		delivery.EntryResolution = workflow.WebhookEntryByPhone
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
	delivery.EntryID, delivery.EntryType = entryID, entryType

	started, err := uc.starter.start(w, trigger, workflow.TriggerEvent{
		WorkspaceID: wh.WorkspaceID,
		EntryID:     entryID,
		EntryType:   entryType,
		TriggerType: workflow.TriggerWebhook,
		StartedBy:   workflow.StartedByWebhook(delivery.ID),
		Data:        workflow.WebhookRunData(req.Method, body, req.RawBody),
	})
	if err != nil {
		return nil, err
	}
	return &WebhookResult{RunID: started.RunID, AlreadyRunning: started.AlreadyRunning}, nil
}

func (uc *handleWebhookTrigger) webhookWorkflow(wh *workflow.WorkflowWebhook) (*workflow.Workflow, *workflow.Node, error) {
	w, err := workflow.LoadPublished(uc.workflowRepo, wh.WorkflowID)
	if err != nil {
		return nil, nil, err
	}
	if !w.Callable() {
		return nil, nil, workflow.ErrWorkflowNotActive
	}
	trigger := w.Graph.TriggerNodeByType(workflow.TriggerWebhook)
	if trigger == nil {
		return nil, nil, workflow.ErrWebhookNoTriggerNode
	}
	return w, trigger, nil
}

func (uc *handleWebhookTrigger) startOnLead(w *workflow.Workflow, wh *workflow.WorkflowWebhook, req WebhookRequest, body map[string]interface{}, delivery *workflow.WebhookDelivery) (*WebhookResult, error) {
	lead, resolution, err := uc.leadOf(wh.WorkspaceID, body)
	if err != nil {
		return nil, err
	}
	delivery.EntryResolution, delivery.EntryID, delivery.EntryType = resolution, lead.ID, workflow.EntryTypeLead
	occurrence := workflow.WebhookOccurrence(stringField(body, "event"), stringField(body, "external_id"), idempotencyKeyOf(req), delivery.ID)
	event := workflow.WebhookLeadEvent(
		workflow.LeadSubject{WorkspaceID: wh.WorkspaceID, LeadID: lead.ID, LeadName: lead.RealName()},
		occurrence,
		workflow.WebhookRunData(req.Method, body, req.RawBody),
	)
	event.StartedBy = workflow.StartedByWebhook(delivery.ID)
	started, err := uc.leadStart.Starter.Start(w, event)
	if err != nil {
		return nil, err
	}
	return &WebhookResult{RunID: started.RunID, Duplicate: started.Duplicate}, nil
}

func (uc *handleWebhookTrigger) leadOf(workspaceID string, body map[string]interface{}) (*lead_domain.Lead, string, error) {
	phone, email := stringField(body, "phone"), stringField(body, "email")
	switch {
	case phone != "":
		normalized := shared.NormalizePhone(phone)
		if normalized == "" {
			return nil, "", workflow.ErrWebhookLeadPhoneInvalid
		}
		lead, err := uc.leadStart.Leads.EnsureLead(context.Background(), workspaceID, normalized, stringField(body, "name"))
		if err != nil {
			return nil, "", err
		}
		return lead, workflow.WebhookEntryByLeadPhone, nil
	case email != "":
		lead, err := uc.leadStart.Leads.FindByEmail(workspaceID, email)
		if errors.Is(err, lead_domain.ErrLeadNotFound) || (err == nil && lead == nil) {
			return nil, "", workflow.ErrWebhookLeadNotFound
		}
		if err != nil {
			return nil, "", err
		}
		return lead, workflow.WebhookEntryByLeadEmail, nil
	}
	return nil, "", workflow.ErrWebhookLeadContactRequired
}

func (uc *handleWebhookTrigger) record(delivery *workflow.WebhookDelivery, result *WebhookResult, err error, received time.Time) {
	switch {
	case err != nil:
		delivery.Outcome = workflow.WebhookOutcomeRejected
		delivery.Reason = workflow.WebhookRejectionReason(err)
		delivery.HTTPStatus = webhookanswer.RejectionStatus(delivery.Reason)
	case result.Duplicate:
		delivery.Outcome = workflow.WebhookOutcomeDuplicate
	case result.AlreadyRunning:
		delivery.Outcome = workflow.WebhookOutcomeAlreadyRunning
	case result.Ignored:
		delivery.Outcome = workflow.WebhookOutcomeIgnored
	default:
		delivery.Outcome = workflow.WebhookOutcomeAccepted
	}
	if err == nil {
		delivery.HTTPStatus = webhookanswer.AcceptStatus(string(delivery.Outcome))
		delivery.RunID = result.RunID
	}
	delivery.LatencyMs = time.Since(received).Milliseconds()
	uc.metrics.IncWorkflowWebhookDelivery(string(delivery.Outcome), delivery.Reason)
	if recordErr := uc.deliveries.Record(delivery); recordErr != nil {
		slog.Error("workflow.webhook.delivery_log_failed",
			slog.String("workspace_id", delivery.WorkspaceID),
			slog.String("workflow_id", delivery.WorkflowID),
			slog.String("delivery_id", delivery.ID),
			slog.String("outcome", string(delivery.Outcome)),
			slog.String("error", recordErr.Error()))
	}
}

type ListWebhookDeliveriesUseCase struct {
	deliveries workflow.WebhookDeliveryRepository
	visibility conversation.Visibility
}

func NewListWebhookDeliveriesUseCase(deliveries workflow.WebhookDeliveryRepository, visibility conversation.Visibility) *ListWebhookDeliveriesUseCase {
	return &ListWebhookDeliveriesUseCase{deliveries: deliveries, visibility: visibility}
}

func (uc *ListWebhookDeliveriesUseCase) Execute(viewer shared.Person, workspaceID, workflowID string, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.WebhookDelivery], error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, workflow.ErrWorkspaceIDRequired
	}
	if strings.TrimSpace(workflowID) == "" {
		return nil, workflow.ErrWorkflowIDRequired
	}
	return uc.deliveries.ListByWorkflow(workspaceID, workflowID, conversationScope(uc.visibility, viewer, workspaceID), pagination)
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

func idempotencyKeyOf(req WebhookRequest) string {
	for _, h := range idempotencyHeaderCandidates {
		if v := strings.TrimSpace(req.Header(h)); v != "" {
			return v
		}
	}
	return ""
}

func dedupKey(wh *workflow.WorkflowWebhook, req WebhookRequest) string {
	if key := idempotencyKeyOf(req); key != "" {
		return "wfwh:" + wh.Token + ":" + key
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
