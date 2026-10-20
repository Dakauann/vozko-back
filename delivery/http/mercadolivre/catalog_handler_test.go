package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/commentautomationhttp"
	"vozko/domain/auth"
	ca "vozko/domain/commentautomation"
	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	mluc "vozko/usecases/mercadolivre"
)

type fakeCatalogListings struct {
	query mluc.ListingsQuery
	url   string
	err   error
}

func (f *fakeCatalogListings) List(_ context.Context, q mluc.ListingsQuery) (*mluc.ListingPage, error) {
	f.query = q
	if f.err != nil {
		return nil, f.err
	}
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	return &mluc.ListingPage{Items: []mluc.ListingView{{
		Listing:      &mldomain.Listing{ID: "lst-1", AccountID: "acct-1", ItemID: "MLB1", Title: "Caneca", Status: "active", SyncedAt: &at},
		ThumbnailURL: "https://cdn/t.jpg",
	}}, NextCursor: "next"}, nil
}

func (f *fakeCatalogListings) ThumbnailURL(context.Context, string, string, string) (string, error) {
	return f.url, f.err
}

type fakeCatalogQuestions struct {
	query   mluc.QuestionsQuery
	answer  mluc.AnswerInput
	deleted string
	blocked string
	got     string
	err     error
}

func (f *fakeCatalogQuestions) Get(_ context.Context, workspaceID, accountID, questionID string) (*mluc.QuestionView, error) {
	f.got = workspaceID + "|" + accountID + "|" + questionID
	if f.err != nil {
		return nil, f.err
	}
	blockedAt := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	return &mluc.QuestionView{
		Question: sampleQuestion(),
		Asker:    &mldomain.Contact{ID: "ct-1", MLUserID: "123", Nickname: "COMPRADOR", QuestionsBlockedAt: &blockedAt},
	}, nil
}

func sampleQuestion() *mldomain.Question {
	return &mldomain.Question{ID: "q-1", AccountID: "acct-1", ItemID: "MLB1", QuestionID: "13000000001", ContactID: "ct-1", Text: "Tem azul?",
		Status: mldomain.QuestionUnanswered, AskedAt: time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC)}
}

func (f *fakeCatalogQuestions) List(_ context.Context, q mluc.QuestionsQuery) (*mluc.QuestionPage, error) {
	f.query = q
	if f.err != nil {
		return nil, f.err
	}
	return &mluc.QuestionPage{Items: []mluc.QuestionView{{
		Question: sampleQuestion(),
		Asker:    &mldomain.Contact{ID: "ct-1", MLUserID: "123", Nickname: "COMPRADOR"},
		Listing:  &mluc.ListingView{Listing: &mldomain.Listing{ID: "lst-1", ItemID: "MLB1", Title: "Caneca"}},
	}}}, nil
}

func (f *fakeCatalogQuestions) Answer(_ context.Context, in mluc.AnswerInput) (*mldomain.Question, error) {
	f.answer = in
	if f.err != nil {
		return nil, f.err
	}
	q := sampleQuestion()
	q.Status, q.AnswerText, q.AnsweredBy, q.AnsweredByUserID = mldomain.QuestionAnswered, in.Text, in.Source, in.UserID
	return q, nil
}

func (f *fakeCatalogQuestions) Delete(_ context.Context, _, _, id string) error {
	f.deleted = id
	return f.err
}

func (f *fakeCatalogQuestions) BlockAsker(_ context.Context, _, _, id string) error {
	f.blocked = "block:" + id
	return f.err
}

func (f *fakeCatalogQuestions) UnblockAsker(_ context.Context, _, _, id string) error {
	f.blocked = "unblock:" + id
	return f.err
}

func (f *fakeCatalogQuestions) ResponseTime(context.Context, string, string) (*mldomain.ResponseTime, error) {
	return &mldomain.ResponseTime{Available: true, Total: &mldomain.ResponseTimeSlot{Minutes: 42}}, f.err
}

type fakeCatalogReasons struct {
	query mluc.ConversationQuery
	send  mluc.SendReasonInput
	err   error
}

func (f *fakeCatalogReasons) Options(_ context.Context, q mluc.ConversationQuery) ([]mldomain.ReasonOption, error) {
	f.query = q
	return []mldomain.ReasonOption{
		{ID: "OTHER", Enabled: true, Actionable: true, CharLimit: 350, CapAvailable: 1},
		{ID: "DELIVERY_PROMISE", Description: "This is an example...", Type: "TEMPLATE", Enabled: true, Actionable: true, CapAvailable: 1, Templates: []mldomain.ReasonTemplate{
			{ID: "TEMPLATE___DELIVERY_PROMISE___1", Text: "Olá,\nEntregaremos sua compra %s entre %d e %d h.", Vars: []mldomain.ReasonVar{{ID: "TEMPLATE___DELIVERY_PROMISE___1___VAR___INIT", Type: "NUMBER"}}},
			{ID: "TEMPLATE___NO_TEXT"},
		}},
	}, f.err
}

func (f *fakeCatalogReasons) Send(_ context.Context, in mluc.SendReasonInput) (*mluc.SendReasonOutput, error) {
	f.send = in
	if f.err != nil {
		return nil, f.err
	}
	return &mluc.SendReasonOutput{ProviderMessageID: "0123456789abcdefaaaa"}, nil
}

type fakeRuleManager struct{ got *ca.Rule }

func (f *fakeRuleManager) List(_ context.Context, ws string, source shared.EntryType, accountID string) ([]*ca.Rule, error) {
	f.got = &ca.Rule{WorkspaceID: ws, Source: source, AccountID: accountID}
	return nil, nil
}
func (f *fakeRuleManager) Create(_ context.Context, r *ca.Rule) (*ca.Rule, error) {
	f.got = r
	return r, nil
}
func (f *fakeRuleManager) Update(_ context.Context, r *ca.Rule) (*ca.Rule, error) {
	f.got = r
	return r, nil
}
func (f *fakeRuleManager) Delete(_ context.Context, ws string, source shared.EntryType, accountID, _ string) error {
	f.got = &ca.Rule{WorkspaceID: ws, Source: source, AccountID: accountID}
	return mldomain.ErrAccountNotFound
}

type catalogTest struct {
	router    *mux.Router
	listings  *fakeCatalogListings
	questions *fakeCatalogQuestions
	reasons   *fakeCatalogReasons
	packs     *fakeCatalogPacks
	rules     *fakeRuleManager
}

func passThrough(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
	return next
}

func newCatalogTest() *catalogTest {
	c := &catalogTest{listings: &fakeCatalogListings{}, questions: &fakeCatalogQuestions{}, reasons: &fakeCatalogReasons{}, packs: &fakeCatalogPacks{}, rules: &fakeRuleManager{}}
	h := NewCatalogHandler(CatalogDeps{Listings: c.listings, Questions: c.questions, Reasons: c.reasons, Packs: c.packs, Rules: c.rules})
	c.router = mux.NewRouter()
	RegisterCatalogRoutes(c.router, h, passThrough)
	return c
}

func (c *catalogTest) do(method, path, body string, signedIn bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	if signedIn {
		ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	}
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestTheQuestionListPassesItsFiltersAndAnswersTheShape(t *testing.T) {
	c := newCatalogTest()
	rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/questions?status=unanswered&itemId=MLB1&cursor=abc&limit=10", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	q := c.questions.query
	if q.WorkspaceID != "ws-1" || q.AccountID != "acct-1" || q.ItemID != "MLB1" || q.Cursor != "abc" || q.Limit != 10 || q.Status == nil || *q.Status != mldomain.QuestionUnanswered {
		t.Fatalf("query = %+v", q)
	}
	var out QuestionListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Data) != 1 {
		t.Fatalf("body = %s", rec.Body)
	}
	if got := out.Data[0]; got.QuestionID != "13000000001" || got.Asker == nil || got.Asker.Nickname != "COMPRADOR" || got.Listing == nil || got.Listing.Title != "Caneca" {
		t.Fatalf("question = %+v", got)
	}

	rec = c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/listings/MLB2/questions", "", true)
	if rec.Code != http.StatusOK || c.questions.query.ItemID != "MLB2" {
		t.Fatalf("per listing = %d %+v", rec.Code, c.questions.query)
	}
	for _, path := range []string{"/mercadolivre/accounts/acct-1/questions?status=OPEN", "/mercadolivre/accounts/acct-1/questions?limit=abc"} {
		if rec := c.do(http.MethodGet, path, "", true); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", path, rec.Code)
		}
	}
	c.questions.err = shared.ErrKeysetInvalid
	if rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/questions?cursor=x", "", true); rec.Code != http.StatusBadRequest {
		t.Fatalf("a forged cursor = %d", rec.Code)
	}
}

func TestAnAnswerIsGivenByTheSignedInOperator(t *testing.T) {
	c := newCatalogTest()
	rec := c.do(http.MethodPost, "/mercadolivre/accounts/acct-1/questions/13000000001/answer", `{"text":"Temos sim"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	a := c.questions.answer
	if a.Source != mldomain.AnsweredByOperator || a.UserID == nil || *a.UserID != "user-1" || a.QuestionID != "13000000001" || a.Text != "Temos sim" || a.WorkspaceID != "ws-1" {
		t.Fatalf("answer = %+v", a)
	}
	if !strings.Contains(rec.Body.String(), `"answeredBy":"operator"`) {
		t.Fatalf("body = %s", rec.Body)
	}
	if rec := c.do(http.MethodPost, "/mercadolivre/accounts/acct-1/questions/13000000001/answer", `{"text":"x"}`, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", rec.Code)
	}
}

func TestQuestionErrorsMapToHTTP(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{mldomain.ErrQuestionNotOpen, http.StatusConflict},
		{mldomain.ErrQuestionNotOwned, http.StatusNotFound},
		{mldomain.ErrAnswerTooLong, http.StatusBadRequest},
		{mldomain.ErrAnswerEmpty, http.StatusBadRequest},
		{mluc.ErrAccountUnavailable, http.StatusConflict},
		{mluc.ErrResourceGone, http.StatusNotFound},
		{mldomain.ErrAccountNotFound, http.StatusNotFound},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		c := newCatalogTest()
		c.questions.err = tc.err
		if rec := c.do(http.MethodPost, "/mercadolivre/accounts/acct-1/questions/13000000001/answer", `{"text":"x"}`, true); rec.Code != tc.code {
			t.Errorf("%v: status = %d", tc.err, rec.Code)
		}
	}
}

func TestDeleteBlockAndUnblockReachTheirUseCases(t *testing.T) {
	c := newCatalogTest()
	if rec := c.do(http.MethodDelete, "/mercadolivre/accounts/acct-1/questions/13000000001", "", true); rec.Code != http.StatusOK || c.questions.deleted != "13000000001" {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := c.do(http.MethodPost, "/mercadolivre/accounts/acct-1/questions/13000000001/block-author", "", true); rec.Code != http.StatusOK || c.questions.blocked != "block:13000000001" {
		t.Fatalf("block = %d %s", rec.Code, c.questions.blocked)
	}
	if rec := c.do(http.MethodDelete, "/mercadolivre/accounts/acct-1/questions/13000000001/block-author", "", true); rec.Code != http.StatusOK || c.questions.blocked != "unblock:13000000001" {
		t.Fatalf("unblock = %d %s", rec.Code, c.questions.blocked)
	}
	rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/questions/response-time", "", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"total":{"minutes":42}`) || !strings.Contains(rec.Body.String(), `"available":true`) {
		t.Fatalf("response time = %d %s", rec.Code, rec.Body)
	}
}

func TestListingsAndTheirThumbnail(t *testing.T) {
	c := newCatalogTest()
	rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/listings?search=caneca&status=active&cursor=c1&limit=20", "", true)
	if rec.Code != http.StatusOK || c.listings.query.Search != "caneca" || c.listings.query.Status != "active" || c.listings.query.Cursor != "c1" || c.listings.query.Limit != 20 {
		t.Fatalf("listings = %d %+v", rec.Code, c.listings.query)
	}
	if !strings.Contains(rec.Body.String(), `"thumbnailUrl":"https://cdn/t.jpg"`) || !strings.Contains(rec.Body.String(), `"nextCursor":"next"`) {
		t.Fatalf("body = %s", rec.Body)
	}
	c.listings.url = "https://cdn/t.jpg"
	rec = c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/listings/MLB1/thumbnail", "", true)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://cdn/t.jpg" {
		t.Fatalf("thumbnail = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	c.listings.err = mldomain.ErrListingNotFound
	if rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/listings/MLB1/thumbnail", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("missing thumbnail = %d", rec.Code)
	}
}

func TestCommentRulesAreScopedToTheAccountInTheURL(t *testing.T) {
	c := newCatalogTest()
	body := `{"name":"Cor","enabled":true,"containerId":"MLB1","match":"any","actions":["public_reply"],"publicReplyText":"Temos"}`
	if rec := c.do(http.MethodPost, "/mercadolivre/accounts/acct-1/comment-rules", body, true); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	if c.rules.got.Source != shared.EntryTypeMercadoLivre || c.rules.got.AccountID != "acct-1" || c.rules.got.WorkspaceID != "ws-1" {
		t.Fatalf("rule = %+v", c.rules.got)
	}
	if rec := c.do(http.MethodPatch, "/mercadolivre/accounts/acct-1/comment-rules/rule-1", body, true); rec.Code != http.StatusOK || c.rules.got.ID != "rule-1" {
		t.Fatalf("update = %d %+v", rec.Code, c.rules.got)
	}
	if rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/comment-rules", "", true); rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("list = %d %q", rec.Code, rec.Body)
	}
	if rec := c.do(http.MethodDelete, "/mercadolivre/accounts/acct-1/comment-rules/rule-1", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("delete on an account of another workspace = %d", rec.Code)
	}
}

func TestReasonsAreAnOperatorAction(t *testing.T) {
	c := newCatalogTest()
	rec := c.do(http.MethodGet, "/mercadolivre/conversations/conv-1/reasons", "", true)
	if rec.Code != http.StatusOK || c.reasons.query.Actor.Kind() != conversation.SenderHuman || c.reasons.query.Actor.ID() != "user-1" || c.reasons.query.EntryID != "conv-1" {
		t.Fatalf("options = %d %+v", rec.Code, c.reasons.query)
	}
	var reasons struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reasons); err != nil || len(reasons.Data) != 2 {
		t.Fatalf("body = %s", rec.Body)
	}
	other, promise := reasons.Data[0], reasons.Data[1]
	if _, has := other["description"]; has || other["capAvailable"] != float64(1) || len(other["templates"].([]any)) != 0 {
		t.Fatalf("other = %v", other)
	}
	templates := promise["templates"].([]any)
	withText, withoutText := templates[0].(map[string]any), templates[1].(map[string]any)
	if promise["description"] != "This is an example..." || withText["text"] != "Olá,\nEntregaremos sua compra %s entre %d e %d h." {
		t.Fatalf("delivery promise = %v", promise)
	}
	if _, has := withoutText["text"]; has {
		t.Fatalf("a template without text must omit it: %v", withoutText)
	}
	if v := withText["vars"].([]any)[0].(map[string]any); v["id"] != "TEMPLATE___DELIVERY_PROMISE___1___VAR___INIT" || v["type"] != "NUMBER" || len(v) != 2 {
		t.Fatalf("var = %v", v)
	}
	rec = c.do(http.MethodPost, "/mercadolivre/conversations/conv-1/reasons", `{"optionId":"REQUEST_VARIANTS","templateId":"TPL","vars":[{"id":"DAYS","value":2}]}`, true)
	if rec.Code != http.StatusCreated || c.reasons.send.OptionID != "REQUEST_VARIANTS" || len(c.reasons.send.Vars) != 1 || c.reasons.send.Vars[0].Value != 2 {
		t.Fatalf("send = %d %+v", rec.Code, c.reasons.send)
	}
	if !strings.Contains(rec.Body.String(), `"providerMessageId":"0123456789abcdefaaaa"`) {
		t.Fatalf("body = %s", rec.Body)
	}
	if rec := c.do(http.MethodPost, "/mercadolivre/conversations/conv-1/reasons", `{}`, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", rec.Code)
	}
	for err, code := range map[error]int{
		mldomain.ErrPersonOnly:           http.StatusForbidden,
		conversation.ErrUnauthorized:     http.StatusForbidden,
		mldomain.ErrReasonCapReached:     http.StatusUnprocessableEntity,
		mldomain.ErrReasonUnknown:        http.StatusUnprocessableEntity,
		mldomain.ErrReasonContent:        http.StatusUnprocessableEntity,
		mldomain.ErrConversationNotFound: http.StatusNotFound,
	} {
		c.reasons.err = err
		if rec := c.do(http.MethodPost, "/mercadolivre/conversations/conv-1/reasons", `{"optionId":"OTHER","text":"oi"}`, true); rec.Code != code {
			t.Errorf("%v = %d, want %d", err, rec.Code, code)
		}
	}
}

var _ commentautomationhttp.RuleManager = (*fakeRuleManager)(nil)

type fakeCatalogPacks struct {
	query mluc.ConversationQuery
	view  *mluc.PackView
	err   error
}

func (f *fakeCatalogPacks) Summary(_ context.Context, q mluc.ConversationQuery) (*mluc.PackView, error) {
	f.query = q
	return f.view, f.err
}

func TestThePackSummaryAnswersTheThreadHeaderShape(t *testing.T) {
	c := newCatalogTest()
	c.packs.view = &mluc.PackView{
		Items: []mluc.PackItemView{
			{ItemID: "MLB1", Title: "Caneca", Quantity: 2, UnitPrice: 19.9, CurrencyID: "BRL", ThumbnailURL: "https://cdn.example/a.jpg", Permalink: "https://produto.mercadolivre.com.br/MLB-1"},
			{ItemID: "MLB2", Title: "Prato", Quantity: 1, UnitPrice: 20, CurrencyID: "BRL"},
		},
		OrderStatus: "paid", ShippingStatus: "shipped", ShippingSubstatus: "out_for_delivery", TotalAmount: 59.8, CurrencyID: "BRL",
		RefreshedAt: time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC),
	}
	rec := c.do(http.MethodGet, "/mercadolivre/conversations/conv-1/pack", "", true)
	if rec.Code != http.StatusOK || c.packs.query.Actor.Kind() != conversation.SenderHuman || c.packs.query.Actor.ID() != "user-1" ||
		c.packs.query.EntryID != "conv-1" || c.packs.query.WorkspaceID != "ws-1" {
		t.Fatalf("pack = %d %+v", rec.Code, c.packs.query)
	}
	want := `{"items":[{"itemId":"MLB1","title":"Caneca","quantity":2,"unitPrice":19.9,"currency":"BRL","thumbnailUrl":"https://cdn.example/a.jpg","permalink":"https://produto.mercadolivre.com.br/MLB-1"},` +
		`{"itemId":"MLB2","title":"Prato","quantity":1,"unitPrice":20,"currency":"BRL"}],` +
		`"orderStatus":"paid","shippingStatus":"shipped","shippingSubstatus":"out_for_delivery","total":59.8,"currency":"BRL","refreshedAt":"2026-10-19T11:00:00Z"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if rec := c.do(http.MethodGet, "/mercadolivre/conversations/conv-1/pack", "", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", rec.Code)
	}
	for err, code := range map[error]int{
		mldomain.ErrPersonOnly:           http.StatusForbidden,
		conversation.ErrUnauthorized:     http.StatusForbidden,
		mldomain.ErrConversationNotFound: http.StatusNotFound,
		mldomain.ErrPackSummaryPending:   http.StatusNotFound,
	} {
		c.packs.err = err
		if rec := c.do(http.MethodGet, "/mercadolivre/conversations/conv-1/pack", "", true); rec.Code != code {
			t.Errorf("%v = %d, want %d", err, rec.Code, code)
		}
	}
	c.packs.err = mldomain.ErrPackSummaryPending
	if rec := c.do(http.MethodGet, "/mercadolivre/conversations/conv-1/pack", "", true); !strings.Contains(rec.Body.String(), `"pack_summary_pending"`) {
		t.Fatalf("pending body = %s", rec.Body)
	}
}

func TestOneQuestionIsReadForARowRefresh(t *testing.T) {
	c := newCatalogTest()
	rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/questions/13000000001", "", true)
	if rec.Code != http.StatusOK || c.questions.got != "ws-1|acct-1|13000000001" {
		t.Fatalf("get = %d %s %s", rec.Code, c.questions.got, rec.Body)
	}
	var out QuestionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.QuestionID != "13000000001" || out.Asker == nil || !out.Asker.Blocked || out.Asker.Nickname != "COMPRADOR" {
		t.Fatalf("question = %+v", out)
	}
	if rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/questions/response-time", "", true); rec.Code != http.StatusOK || c.questions.got != "ws-1|acct-1|13000000001" {
		t.Fatal("the response time route must not be read as a question id")
	}
	for err, code := range map[error]int{
		mldomain.ErrQuestionNotFound: http.StatusNotFound,
		mldomain.ErrQuestionNotOwned: http.StatusNotFound,
		mldomain.ErrAccountNotFound:  http.StatusNotFound,
	} {
		c.questions.err = err
		if rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/questions/13000000001", "", true); rec.Code != code {
			t.Errorf("%v = %d, want %d", err, rec.Code, code)
		}
	}
}

func TestTheQuestionListSaysWhetherTheAskerIsBlocked(t *testing.T) {
	c := newCatalogTest()
	rec := c.do(http.MethodGet, "/mercadolivre/accounts/acct-1/questions", "", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"asker":{"contactId":"ct-1","mlUserId":"123","nickname":"COMPRADOR","blocked":false}`) {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
}
