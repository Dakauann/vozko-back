package template_usecase

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

// ---------------------------------------------------------------- test doubles

// fakeAttempts models the ONE property the real table provides: a unique index
// on (workspace, key), so exactly one caller may create a given attempt. If this
// fake were permissive the tests below would pass while the feature double
// charged.
type fakeAttempts struct {
	mu     sync.Mutex
	byKey  map[string]*template.SendAttempt
	byID   map[string]*template.SendAttempt
	charge int
}

func newFakeAttempts() *fakeAttempts {
	return &fakeAttempts{byKey: map[string]*template.SendAttempt{}, byID: map[string]*template.SendAttempt{}}
}

func (f *fakeAttempts) CreateIfAbsent(_ context.Context, a *template.SendAttempt) (*template.SendAttempt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := a.WorkspaceID + "|" + a.IdempotencyKey
	if existing, ok := f.byKey[k]; ok {
		copied := *existing
		return &copied, false, nil
	}
	stored := *a
	stored.UpdatedAt = time.Now().UTC()
	f.byKey[k] = &stored
	f.byID[stored.ID] = &stored
	copied := stored
	return &copied, true, nil
}

func (f *fakeAttempts) FindByID(_ context.Context, id string) (*template.SendAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeAttempts) FindByIdempotencyKey(_ context.Context, ws, key string) (*template.SendAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byKey[ws+"|"+key]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeAttempts) FindByProviderMessageID(context.Context, string, string) (*template.SendAttempt, error) {
	return nil, nil
}

// transition enforces the same guard the real UPDATE does, so a losing writer
// gets a conflict here exactly as it would in Postgres.
func (f *fakeAttempts) transition(id string, next template.SendAttemptStatus, apply func(*template.SendAttempt)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return template.ErrSendAttemptConflict
	}
	if !a.Status.CanTransitionTo(next) {
		return template.ErrSendAttemptConflict
	}
	a.Status = next
	a.UpdatedAt = time.Now().UTC()
	if apply != nil {
		apply(a)
	}
	return nil
}

func (f *fakeAttempts) MarkCharged(_ context.Context, id string, micros int64, at time.Time) error {
	return f.transition(id, template.SendAttemptCharged, func(a *template.SendAttempt) {
		a.ChargedMicros = micros
		a.ChargedAt = &at
		f.charge++
	})
}

func (f *fakeAttempts) MarkSent(_ context.Context, id, msgID string, status int, at time.Time) error {
	return f.transition(id, template.SendAttemptSent, func(a *template.SendAttempt) {
		a.ProviderMessageID = msgID
		a.ResponseStatus = status
		a.SentAt = &at
	})
}

func (f *fakeAttempts) MarkRejected(_ context.Context, id string, code int, msg string, status int) error {
	return f.transition(id, template.SendAttemptRejected, func(a *template.SendAttempt) {
		a.ErrorCode, a.ErrorMessage, a.ResponseStatus = code, msg, status
	})
}

func (f *fakeAttempts) MarkUnknown(_ context.Context, id, msg string, status int) error {
	return f.transition(id, template.SendAttemptUnknown, func(a *template.SendAttempt) {
		a.ErrorMessage, a.ResponseStatus = msg, status
	})
}

func (f *fakeAttempts) MarkRefunded(_ context.Context, id string, at time.Time) error {
	return f.transition(id, template.SendAttemptRefunded, func(a *template.SendAttempt) { a.RefundedAt = &at })
}

// ListNeedingReconciliation mirrors the real query: attempts that took money and
// never settled, oldest first. Without this the sweep's tests would pass against
// an empty list and prove nothing.
func (f *fakeAttempts) ListNeedingReconciliation(_ context.Context, olderThan time.Time, limit int) ([]*template.SendAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*template.SendAttempt
	for _, a := range f.byID {
		if a.Status != template.SendAttemptCharged && a.Status != template.SendAttemptUnknown {
			continue
		}
		if !a.UpdatedAt.Before(olderThan) {
			continue
		}
		copied := *a
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// seed installs an attempt in a chosen state, for tests about what happens after
// the send rather than during it.
func (f *fakeAttempts) seed(a template.SendAttempt) *template.SendAttempt {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := a
	if stored.UpdatedAt.IsZero() {
		stored.UpdatedAt = time.Now().UTC()
	}
	f.byID[stored.ID] = &stored
	f.byKey[stored.WorkspaceID+"|"+stored.IdempotencyKey] = &stored
	return &stored
}

func (f *fakeAttempts) statusOf(id string) template.SendAttemptStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; ok {
		return a.Status
	}
	return ""
}

// countingBilling records every movement so a test can assert on the NUMBER of
// charges, which is the only assertion that catches a double charge.
type countingBilling struct {
	mu               sync.Mutex
	debits           []string
	refunds          []string
	refundCategories []string
	cost             int64
	costErr          error
	debitErr         error
	refundErr        error
}

func (c *countingBilling) Execute(_ string, ref string, _ string) (*balance.Transaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.debitErr != nil {
		return nil, c.debitErr
	}
	c.debits = append(c.debits, ref)
	return &balance.Transaction{}, nil
}

func (c *countingBilling) Refund(_ string, ref string, category string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refundErr != nil {
		return c.refundErr
	}
	c.refunds = append(c.refunds, ref)
	c.refundCategories = append(c.refundCategories, category)
	return nil
}

func (c *countingBilling) GetTemplateCostMicros(string, string) (int64, error) {
	return c.cost, c.costErr
}

func (c *countingBilling) debitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.debits)
}

func (c *countingBilling) refundCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.refunds)
}

// fakeLedger answers "was this reference already charged", the belt.
type fakeLedger struct {
	balance.Repository
	mu   sync.Mutex
	refs map[string]bool
}

func (l *fakeLedger) ExistsTransactionByReferenceID(ref string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.refs[ref], nil
}

type fakeInflight struct{ reserved, released int64 }

func (f *fakeInflight) Reserve(_ string, delta, _ int64) (bool, error) {
	f.reserved += delta
	return true, nil
}
func (f *fakeInflight) Release(_ string, delta int64) error    { f.released += delta; return nil }
func (f *fakeInflight) RefreshTTL(string, time.Duration) error { return nil }
func (f *fakeInflight) GetInflight(string) (int64, error)      { return 0, nil }

type fakeBalanceChecker struct{ micros int64 }

func (f *fakeBalanceChecker) HasSufficientBalance(string, int64) (bool, error) { return true, nil }
func (f *fakeBalanceChecker) GetBalance(string) (int64, error)                 { return f.micros, nil }
func (f *fakeBalanceChecker) Invalidate(string)                                {}
func (f *fakeBalanceChecker) InvalidateDebounced(string)                       {}

type stubTemplateRepo struct {
	template.Repository
	tmpl *template.Template
}

func (s *stubTemplateRepo) FindByID(string) (*template.Template, error) { return s.tmpl, nil }

type stubClient struct {
	conversation.WhatsAppClient
	mu     sync.Mutex
	calls  int
	out    *conversation.SendTextMessageOutput
	err    error
	lastIn conversation.SendTemplateMessageInput
}

func (s *stubClient) SendTemplateMessage(_ context.Context, in conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastIn = in
	return s.out, s.err
}

func (s *stubClient) sendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type stubFactory struct {
	client conversation.WhatsAppClient
	waba   string
}

func (s *stubFactory) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	return s.client, nil
}
func (s *stubFactory) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	return s.client, nil
}
func (s *stubFactory) WABAIdForPhone(string) (string, error) { return s.waba, nil }

// ---------------------------------------------------------------- harness

type harness struct {
	uc       template.BilledTemplateSendUseCase
	attempts *fakeAttempts
	billing  *countingBilling
	client   *stubClient
	ledger   *fakeLedger
	inflight *fakeInflight
}

func newHarness(t *testing.T, mutate ...func(*BilledTemplateSenderDeps)) *harness {
	t.Helper()
	attempts := newFakeAttempts()
	billing := &countingBilling{cost: 5_000}
	client := &stubClient{out: &conversation.SendTextMessageOutput{MessageID: "wamid.1", ResponseStatus: 200}}
	ledger := &fakeLedger{refs: map[string]bool{}}
	inflight := &fakeInflight{}

	deps := BilledTemplateSenderDeps{
		Templates:      &stubTemplateRepo{tmpl: approvedUtility()},
		Attempts:       attempts,
		ClientFactory:  &stubFactory{client: client, waba: "waba-1"},
		Consume:        billing,
		Ledger:         ledger,
		Inflight:       inflight,
		BalanceChecker: &fakeBalanceChecker{micros: 1_000_000},
	}
	for _, m := range mutate {
		m(&deps)
	}
	uc, err := NewBilledTemplateSendUseCase(deps)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	return &harness{uc: uc, attempts: attempts, billing: billing, client: client, ledger: ledger, inflight: inflight}
}

func approvedUtility() *template.Template {
	return &template.Template{
		ID: "tpl-1", WABAId: "waba-1", Name: "aviso_agenda", Language: "pt_BR",
		Category: template.TemplateCategoryUtility, Status: template.TemplateStatusApproved,
	}
}

func validInput() template.BilledSendInput {
	return template.BilledSendInput{
		WorkspaceID:     "ws-1",
		UserID:          "user-1",
		IdempotencyKey:  "key-1",
		BusinessPhoneID: "bp-1",
		TemplateID:      "tpl-1",
		ToNumber:        "5511999999999",
	}
}

// ---------------------------------------------------------------- the money

// F-01. The failure this whole type exists to make unrepresentable.
func TestBilledSend_EmptyWorkspace_RefusedBeforeAnyClientCall(t *testing.T) {
	h := newHarness(t)
	in := validInput()
	in.WorkspaceID = ""

	if _, err := h.uc.Execute(context.Background(), in); !errors.Is(err, template.ErrWorkspaceRequired) {
		t.Fatalf("want ErrWorkspaceRequired, got %v", err)
	}
	if h.client.sendCount() != 0 {
		t.Fatal("a send with nobody to bill must never reach the provider")
	}
	if h.billing.debitCount() != 0 {
		t.Fatal("nothing may be charged")
	}
}

func TestBilledSend_MissingIdempotencyKey_Refused(t *testing.T) {
	h := newHarness(t)
	in := validInput()
	in.IdempotencyKey = ""

	if _, err := h.uc.Execute(context.Background(), in); !errors.Is(err, template.ErrIdempotencyKeyRequired) {
		t.Fatalf("want ErrIdempotencyKeyRequired, got %v", err)
	}
	if h.client.sendCount() != 0 {
		t.Fatal("a send that cannot be deduplicated must not happen")
	}
}

// F-02/F-03. The headline guarantee.
func TestBilledSend_SameIdempotencyKey_ChargesOnceSendsOnce(t *testing.T) {
	h := newHarness(t)

	first, err := h.uc.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if first.Replayed {
		t.Fatal("the first send is not a replay")
	}

	second, err := h.uc.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !second.Replayed {
		t.Fatal("the second send must report itself as a replay")
	}
	if second.MessageID != first.MessageID {
		t.Fatalf("a replay must answer with the original result, got %q want %q", second.MessageID, first.MessageID)
	}
	if got := h.billing.debitCount(); got != 1 {
		t.Fatalf("charged %d times, want exactly 1", got)
	}
	if got := h.client.sendCount(); got != 1 {
		t.Fatalf("sent %d times, want exactly 1", got)
	}
}

// F-02 under concurrency: the double-clicked button, two replicas.
func TestBilledSend_ConcurrentSameKey_OneChargeOneSend(t *testing.T) {
	h := newHarness(t)

	const callers = 20
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, _ = h.uc.Execute(context.Background(), validInput())
		}()
	}
	wg.Wait()

	if got := h.billing.debitCount(); got != 1 {
		t.Fatalf("%d concurrent identical requests produced %d charges, want 1", callers, got)
	}
	if got := h.client.sendCount(); got != 1 {
		t.Fatalf("%d concurrent identical requests produced %d sends, want 1", callers, got)
	}
}

// F-04. The bug that was live in this codebase: refunding a delivered message.
func TestBilledSend_ProviderAcceptedButUnreadable_ChargeStands(t *testing.T) {
	h := newHarness(t, func(d *BilledTemplateSenderDeps) {})
	h.client.out = &conversation.SendTextMessageOutput{ResponseStatus: 200}
	h.client.err = errors.Join(conversation.ErrSendOutcomeUnknown, errors.New("bad json"))

	result, err := h.uc.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("an accepted send must not surface as an error: %v", err)
	}
	if result.Status != template.SendAttemptSent {
		t.Fatalf("status = %q, want sent", result.Status)
	}
	if h.billing.refundCount() != 0 {
		t.Fatal("refunding here credits a message the customer already received")
	}
	if h.billing.debitCount() != 1 {
		t.Fatal("the charge must stand")
	}
}

// F-09. A refusal is refunded, and refunded once.
func TestBilledSend_ProviderRejected_RefundsExactlyOnce(t *testing.T) {
	h := newHarness(t)
	h.client.out = &conversation.SendTextMessageOutput{
		ResponseStatus:  400,
		ResponsePayload: []byte(`{"error":{"code":132015,"message":"Template is paused"}}`),
	}
	h.client.err = errors.New("bad request")

	result, err := h.uc.Execute(context.Background(), validInput())
	if err == nil {
		t.Fatal("a rejected send must surface as an error")
	}
	if result == nil || result.Status != template.SendAttemptRefunded {
		t.Fatalf("result = %+v, want a refunded attempt", result)
	}
	if got := h.billing.refundCount(); got != 1 {
		t.Fatalf("refunded %d times, want 1", got)
	}
	if result.ChargedMicros != 0 {
		t.Fatal("a refunded send costs nothing and must report so")
	}
}

// Not knowing is not the same as failing. Refunding here risks crediting a
// delivered message; the reconcile sweep decides later.
func TestBilledSend_TransportUnknown_NoRefund_MarkedUnknown(t *testing.T) {
	h := newHarness(t)
	h.client.out = &conversation.SendTextMessageOutput{ResponseStatus: 503}
	h.client.err = errors.New("upstream unavailable")

	result, _ := h.uc.Execute(context.Background(), validInput())
	if result == nil || result.Status != template.SendAttemptUnknown {
		t.Fatalf("result = %+v, want unknown", result)
	}
	if h.billing.refundCount() != 0 {
		t.Fatal("an unknown outcome must not be refunded on the spot")
	}
}

// F-05. An unusable template is refused BEFORE the debit, not charged and
// refunded.
func TestBilledSend_TemplateNotReady_NoCharge(t *testing.T) {
	notApproved := approvedUtility()
	notApproved.Status = template.TemplateStatusPending
	h := newHarness(t, func(d *BilledTemplateSenderDeps) {
		d.Templates = &stubTemplateRepo{tmpl: notApproved}
	})

	if _, err := h.uc.Execute(context.Background(), validInput()); !errors.Is(err, template.ErrTemplateNotSendable) {
		t.Fatalf("want ErrTemplateNotSendable, got %v", err)
	}
	if h.billing.debitCount() != 0 || h.client.sendCount() != 0 {
		t.Fatal("an unusable template must cost nothing")
	}
}

// F-08. The fail-open that used to send for free.
func TestBilledSend_ZeroPrice_RefusesInsteadOfSendingFree(t *testing.T) {
	h := newHarness(t)
	h.billing.cost = 0

	if _, err := h.uc.Execute(context.Background(), validInput()); !errors.Is(err, template.ErrPricingUnavailable) {
		t.Fatalf("want ErrPricingUnavailable, got %v", err)
	}
	if h.client.sendCount() != 0 {
		t.Fatal("an unpriced workspace must STOP, not send for free")
	}
}

// F-07. Template and number must belong to the same WhatsApp Business Account.
func TestBilledSend_TemplateFromAnotherWABA_Refused(t *testing.T) {
	h := newHarness(t, func(d *BilledTemplateSenderDeps) {
		d.ClientFactory = &stubFactory{client: &stubClient{}, waba: "waba-somebody-else"}
	})

	if _, err := h.uc.Execute(context.Background(), validInput()); !errors.Is(err, template.ErrTemplatePhoneMismatch) {
		t.Fatalf("want ErrTemplatePhoneMismatch, got %v", err)
	}
	if h.billing.debitCount() != 0 {
		t.Fatal("a cross-tenant send must cost nothing")
	}
}

// F-21. A mis-wired container must not produce a sender that sends for free.
func TestBilledSend_MissingBillingDependency_ConstructorRefuses(t *testing.T) {
	_, err := NewBilledTemplateSendUseCase(BilledTemplateSenderDeps{
		Templates:     &stubTemplateRepo{tmpl: approvedUtility()},
		Attempts:      newFakeAttempts(),
		ClientFactory: &stubFactory{},
		// no billing at all
	})
	if !errors.Is(err, template.ErrBillingNotConfigured) {
		t.Fatalf("want ErrBillingNotConfigured, got %v", err)
	}
}

// The belt: a resumed attempt whose debit already committed must not debit again.
func TestBilledSend_ExistingDebitForAttempt_IsNotChargedTwice(t *testing.T) {
	h := newHarness(t)

	// First call dies after the debit but before the provider call.
	h.client.err = errors.New("process died")
	h.client.out = nil
	_, _ = h.uc.Execute(context.Background(), validInput())
	if h.billing.debitCount() != 1 {
		t.Fatalf("setup: want one debit, got %d", h.billing.debitCount())
	}
	// The ledger now knows about that charge.
	for _, ref := range h.billing.debits {
		h.ledger.refs[ref] = true
	}

	// A retry with the same key finds the attempt in `unknown` and replays it
	// rather than charging again.
	h.client.err = nil
	h.client.out = &conversation.SendTextMessageOutput{MessageID: "wamid.2", ResponseStatus: 200}
	if _, err := h.uc.Execute(context.Background(), validInput()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := h.billing.debitCount(); got != 1 {
		t.Fatalf("charged %d times across a crash and a retry, want 1", got)
	}
}

// The correlation id is what lets a delivery-status webhook find the charge.
func TestBilledSend_SendsAttemptIDAsCallbackData(t *testing.T) {
	h := newHarness(t)

	result, err := h.uc.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if h.client.lastIn.BizOpaqueCallbackData != result.AttemptID {
		t.Fatalf("callback data = %q, want the attempt id %q",
			h.client.lastIn.BizOpaqueCallbackData, result.AttemptID)
	}
}

// The reservation must always be given back, including on the failure paths.
func TestBilledSend_ReleasesInflightReservation(t *testing.T) {
	h := newHarness(t)
	if _, err := h.uc.Execute(context.Background(), validInput()); err != nil {
		t.Fatalf("send: %v", err)
	}
	if h.inflight.reserved != h.inflight.released {
		t.Fatalf("reserved %d but released %d", h.inflight.reserved, h.inflight.released)
	}
}
