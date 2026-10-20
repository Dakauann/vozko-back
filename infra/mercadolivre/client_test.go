package mercadolivre

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

func getRequest() request {
	return request{method: http.MethodGet, path: "/users/me", family: FamilyOther, creds: sellerCreds}
}

func postRequest() request {
	return request{method: http.MethodPost, path: "/answers", family: FamilyQuestions, creds: sellerCreds, body: []byte(`{}`), contentType: contentTypeJSON}
}

func TestNewClientRequiresALimiterFactory(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("a client without a rate limiter factory must not be built")
	}
}

func TestTheClientBaseURLIsAnAbsoluteHTTPURLWhenSet(t *testing.T) {
	limiters := newFakeLimiters()
	for _, bad := range []string{"api.mercadolibre.com", "ftp://api.example.com", "https://", "http:///path", "https://api.example.com?x=1", "https://api.example.com#top", "://x"} {
		if _, err := NewClient(Config{BaseURL: bad, RateLimiterFactory: limiters.factory()}); err == nil {
			t.Errorf("base url %q accepted", bad)
		}
	}
	cases := map[string]string{
		"":                           DefaultBaseURL,
		"  ":                         DefaultBaseURL,
		"http://127.0.0.1:8089/":     "http://127.0.0.1:8089",
		"https://ml-fake.example/v1": "https://ml-fake.example/v1",
	}
	for raw, want := range cases {
		c, err := NewClient(Config{BaseURL: raw, RateLimiterFactory: limiters.factory()})
		if err != nil || c.baseURL != want {
			t.Errorf("base url %q = %q, %v", raw, c.baseURL, err)
		}
	}
}

func TestEveryCallPassesTheAccountAndTheAppLimiter(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	limiters := newFakeLimiters()
	c := newTestClient(t, fs, limiters)
	if _, err := c.do(context.Background(), getRequest()); err != nil {
		t.Fatalf("do: %v", err)
	}
	got := limiters.keys()
	if len(got) != 2 || got[0] != "ml:415458330" || got[1] != "ml:app:other" {
		t.Fatalf("limiter keys = %v", got)
	}
	r := fs.last()
	assertBearer(t, r)
	if r.Header.Get("Accept") != "application/json" {
		t.Fatalf("Accept = %q", r.Header.Get("Accept"))
	}
}

func TestBudgetsStayUnderTheDocumentedMessagingLimit(t *testing.T) {
	limiters := newFakeLimiters()
	fs := newFakeServer(t, jsonReply(200, `{}`))
	newTestClient(t, fs, limiters)
	for _, family := range []Family{FamilyMessagesRead, FamilyMessagesWrite} {
		budget, ok := limiters.budgets[family.limiterPrefix()]
		if !ok || budget <= 0 || budget >= 500 {
			t.Errorf("%s budget = %d, must be positive and under 500 per minute", family, budget)
		}
		if limiters.windows[family.limiterPrefix()] != time.Minute {
			t.Errorf("%s window = %v", family, limiters.windows[family.limiterPrefix()])
		}
	}
	for _, family := range Families() {
		if _, ok := limiters.budgets[family.limiterPrefix()]; !ok {
			t.Errorf("family %s has no limiter", family)
		}
	}
	if _, ok := limiters.budgets[accountLimiterPrefix]; !ok {
		t.Error("no per account limiter")
	}
}

func TestALimiterErrorRefusesTheCall(t *testing.T) {
	for _, key := range []string{"ml:415458330", "ml:app:other"} {
		t.Run(key, func(t *testing.T) {
			fs := newFakeServer(t, jsonReply(200, `{}`))
			limiters := newFakeLimiters()
			limiters.fail[key] = errors.New("redis down")
			c := newTestClient(t, fs, limiters)
			_, err := c.do(context.Background(), getRequest())
			if err == nil {
				t.Fatal("a limiter error must never allow the call")
			}
			if fs.count() != 0 {
				t.Fatalf("%d requests reached Mercado Livre", fs.count())
			}
			if !IsRetryable(err) || IsRateLimit(err) {
				t.Fatalf("limiter error must be transient and distinct from a rate limit: %v", err)
			}
		})
	}
}

func TestALongLocalLimitRefusesWithoutWaiting(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	limiters := newFakeLimiters()
	limiters.deny["ml:app:other"] = 30 * time.Second
	c := newTestClient(t, fs, limiters)
	start := time.Now()
	_, err := c.do(context.Background(), getRequest())
	if !IsRateLimit(err) || !IsRetryable(err) {
		t.Fatalf("err = %v, want a local rate limit", err)
	}
	if fs.count() != 0 || time.Since(start) > time.Second {
		t.Fatalf("requests %d, waited %v", fs.count(), time.Since(start))
	}
}

func TestAShortLocalLimitWaitsAndProceeds(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	limiters := newFakeLimiters()
	limiters.deny["ml:415458330"] = 10 * time.Millisecond
	limiters.denyFor["ml:415458330"] = 1
	c := newTestClient(t, fs, limiters)
	if _, err := c.do(context.Background(), getRequest()); err != nil {
		t.Fatalf("do: %v", err)
	}
	if fs.count() != 1 {
		t.Fatalf("requests = %d", fs.count())
	}
}

func TestMissingCredentialsRefuseBeforeAnyCall(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	limiters := newFakeLimiters()
	c := newTestClient(t, fs, limiters)
	req := getRequest()
	req.creds = mldomain.Credentials{UserID: "415458330"}
	if _, err := c.do(context.Background(), req); !errors.Is(err, mldomain.ErrCredentialsInvalid) {
		t.Fatalf("err = %v", err)
	}
	if fs.count() != 0 || len(limiters.keys()) != 0 {
		t.Fatal("nothing may run without credentials")
	}
}

func TestRetryRules(t *testing.T) {
	cases := []struct {
		name     string
		req      request
		statuses []int
		attempts int
		ok       bool
	}{
		{"get retried on 500", getRequest(), []int{500, 200}, 2, true},
		{"get retried on 429", getRequest(), []int{429, 200}, 2, true},
		{"get gives up after three attempts", getRequest(), []int{503, 503, 503, 200}, 3, false},
		{"get not retried on 404", getRequest(), []int{404, 200}, 1, false},
		{"post not retried on 500", postRequest(), []int{500, 200}, 1, false},
		{"post not retried on 502", postRequest(), []int{502, 200}, 1, false},
		{"post retried on 429", postRequest(), []int{429, 200}, 2, true},
		{"post not retried on 400", postRequest(), []int{400, 200}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
				status := tc.statuses[len(tc.statuses)-1]
				if attempt <= len(tc.statuses) {
					status = tc.statuses[attempt-1]
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"message":"x","error":"y","status":`+strconv.Itoa(status)+`,"cause":[]}`)
			})
			limiters := newFakeLimiters()
			c := newTestClient(t, fs, limiters)
			_, err := c.do(context.Background(), tc.req)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v", err)
			}
			if fs.count() != tc.attempts {
				t.Fatalf("attempts = %d, want %d", fs.count(), tc.attempts)
			}
			if got := len(limiters.keys()); got != 2*tc.attempts {
				t.Fatalf("limiter checks = %d, every attempt passes both limiters", got)
			}
		})
	}
}

func TestGetRetriesATransportError(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			hijackAndClose(t, w)
			return
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{}`)
	})
	c := newTestClient(t, fs, newFakeLimiters())
	if _, err := c.do(context.Background(), getRequest()); err != nil {
		t.Fatalf("do: %v", err)
	}
	if fs.count() != 2 {
		t.Fatalf("attempts = %d", fs.count())
	}
}

func TestPostIsNeverRetriedOnATransportError(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			hijackAndClose(t, w)
			return
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{}`)
	})
	c := newTestClient(t, fs, newFakeLimiters())
	_, err := c.do(context.Background(), postRequest())
	if err == nil || fs.count() != 1 {
		t.Fatalf("err = %v, attempts = %d", err, fs.count())
	}
	if !IsAmbiguous(err) {
		t.Fatalf("a post that failed in transport may have acted: %v", err)
	}
}

func TestPostIsNeverRetriedOnATimeout(t *testing.T) {
	release := make(chan struct{})
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			select {
			case <-release:
			case <-time.After(2 * time.Second):
			}
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{}`)
	})
	defer close(release)
	limiters := newFakeLimiters()
	c, err := NewClient(Config{
		BaseURL:            fs.srv.URL,
		HTTPClient:         &http.Client{Timeout: 100 * time.Millisecond},
		RateLimiterFactory: limiters.factory(),
		Backoff:            func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.do(context.Background(), postRequest())
	if err == nil || fs.count() != 1 || !IsAmbiguous(err) {
		t.Fatalf("err = %v, attempts = %d", err, fs.count())
	}
}

func TestACancelledContextStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		cancel()
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{}`)
	})
	limiters := newFakeLimiters()
	c, err := NewClient(Config{
		BaseURL:            fs.srv.URL,
		HTTPClient:         fs.srv.Client(),
		RateLimiterFactory: limiters.factory(),
		Backoff:            func(int) time.Duration { return 50 * time.Millisecond },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.do(ctx, getRequest()); err == nil {
		t.Fatal("expected an error")
	}
	if fs.count() != 1 {
		t.Fatalf("attempts = %d", fs.count())
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		code       string
		message    string
		auth       bool
		permission bool
		rateLimit  bool
		blocked    bool
		substatus  string
		appBlocked bool
		operator   bool
		notFound   bool
		retryable  bool
	}{
		{name: "invalid grant", status: 400, body: `{"error_description":"Error validating grant.","error":"invalid_grant","status":400,"cause":[]}`,
			code: "invalid_grant", message: "Error validating grant.", auth: true},
		{name: "expired token", status: 401, body: `{"message":"invalid access token","error":"invalid_token","status":401,"cause":[]}`,
			code: "invalid_token", message: "invalid access token", auth: true},
		{name: "unauthorized client", status: 400, body: `{"error":"unauthorized_client","message":"no grant","status":400}`,
			code: "unauthorized_client", auth: true, message: "no grant"},
		{name: "operator user", status: 400, body: `{"error":"invalid_operator_user_id","message":"operator","status":400}`,
			code: "invalid_operator_user_id", message: "operator", operator: true},
		{name: "invalid client is the app, not the seller", status: 401, body: `{"error":"invalid_client","message":"bad secret","status":401}`,
			code: "invalid_client", message: "bad secret", appBlocked: true},
		{name: "blocked application", status: 401, body: `{"error":"unauthorized_application","message":"blocked","status":401}`,
			code: "unauthorized_application", message: "blocked", appBlocked: true},
		{name: "missing functional permission", status: 403, body: `{"message":"forbidden","error":"PA_UNAUTHORIZED_RESULT_FROM_POLICIES","status":403,"cause":[]}`,
			code: "PA_UNAUTHORIZED_RESULT_FROM_POLICIES", message: "forbidden", permission: true},
		{name: "permission code in the cause", status: 403, body: `{"message":"denied","error":"forbidden","status":403,"cause":["PA_UNAUTHORIZED_RESULT_FROM_POLICIES"]}`,
			code: "forbidden", message: "denied", permission: true},
		{name: "rate limited", status: 429, body: `{"message":"too many","error":"local_rate_limited","status":429,"cause":[]}`,
			code: "local_rate_limited", message: "too many", rateLimit: true, retryable: true},
		{name: "mediation in process", status: 403, body: `{"status_code":403,"code":"forbidden","message":"You can not send the message because a mediation is in process"}`,
			code: "forbidden", message: "You can not send the message because a mediation is in process", blocked: true, substatus: "blocked_by_mediation"},
		{name: "full not delivered", status: 403, body: `{"status_code":403,"code":"forbidden","message":"You can not send the message because the purchase is Mercado Envíos Full and has not been yet delivered"}`,
			code: "forbidden", message: "You can not send the message because the purchase is Mercado Envíos Full and has not been yet delivered", blocked: true, substatus: "blocked_by_fulfillment"},
		{name: "cancelled order", status: 403, body: `{"status_code":403,"code":"forbidden","message":"You can not send messages on a cancelled order"}`,
			code: "forbidden", message: "You can not send messages on a cancelled order", blocked: true, substatus: "blocked_by_cancelled_order"},
		{name: "generic blocked conversation", status: 403, body: `{"status_code":403,"code":"forbidden","message":"blocked_conversation_send_message_forbidden"}`,
			code: "forbidden", message: "blocked_conversation_send_message_forbidden", blocked: true},
		{name: "substatus named in the cause", status: 400, body: `{"cause":"blocked_by_excepted_case","error":"bad_request","message":"use messages","status_code":400}`,
			code: "bad_request", message: "use messages", blocked: false},
		{name: "known substatus named in the message", status: 403, body: `{"code":"forbidden","message":"conversation blocked_by_mediation_fbm"}`,
			code: "forbidden", message: "conversation blocked_by_mediation_fbm", blocked: true, substatus: "blocked_by_mediation_fbm"},
		{name: "started by seller is not a documented substatus", status: 403, body: `{"code":"forbidden","message":"blocked_by_conversation_started_by_seller"}`,
			code: "forbidden", message: "blocked_by_conversation_started_by_seller", blocked: true},
		{name: "deactivated buyer", status: 451, body: `{"message":"unavailable"}`,
			message: "unavailable", blocked: true, substatus: "blocked_by_deactivated_account"},
		{name: "not found", status: 404, body: `{"message":"Response time not found for user id: 276274936","error":"not_found","status":404,"cause":[]}`,
			code: "not_found", message: "Response time not found for user id: 276274936", notFound: true},
		{name: "server error", status: 500, body: `not json`, message: "not json", retryable: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseError(tc.status, []byte(tc.body))
			var mlErr *Error
			if !errors.As(err, &mlErr) {
				t.Fatalf("not an *Error: %v", err)
			}
			if mlErr.HTTPStatus != tc.status || mlErr.Code != tc.code || mlErr.Message != tc.message {
				t.Fatalf("error = %+v", mlErr)
			}
			sub, blocked := IsBlocked(err)
			if IsAuth(err) != tc.auth || IsPermission(err) != tc.permission || IsRateLimit(err) != tc.rateLimit ||
				blocked != tc.blocked || sub != tc.substatus || IsAppBlocked(err) != tc.appBlocked ||
				IsOperatorUser(err) != tc.operator || IsNotFound(err) != tc.notFound || IsRetryable(err) != tc.retryable {
				t.Fatalf("auth=%v permission=%v rate=%v blocked=%v(%q) app=%v operator=%v notfound=%v retryable=%v",
					IsAuth(err), IsPermission(err), IsRateLimit(err), blocked, sub, IsAppBlocked(err), IsOperatorUser(err), IsNotFound(err), IsRetryable(err))
			}
			if strings.Contains(err.Error(), sellerCreds.AccessToken) {
				t.Fatal("error text must never carry a token")
			}
		})
	}
}

func TestModerationPendingIsA202(t *testing.T) {
	err := &Error{HTTPStatus: http.StatusAccepted}
	if !IsModerationPending(err) || IsModerationPending(&Error{HTTPStatus: 200}) || IsModerationPending(errors.New("x")) {
		t.Fatal("IsModerationPending mismatch")
	}
}

func TestPredicatesIgnoreForeignErrors(t *testing.T) {
	err := errors.New("boom")
	if _, blocked := IsBlocked(err); blocked || IsAuth(err) || IsPermission(err) || IsRateLimit(err) || IsRetryable(err) {
		t.Fatal("a foreign error must classify as nothing")
	}
}

func TestEncodeJSONIsPureASCII(t *testing.T) {
	raw, err := encodeJSON(map[string]string{"text": "Olá \U0001F600 <a href=\"x\">rastreio</a>"})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range raw {
		if b >= 0x80 {
			t.Fatalf("non ASCII byte in %s", raw)
		}
	}
	bs := string(rune(0x5C))
	want := `{"text":"Ol` + bs + `u00e1 ` + bs + `ud83d` + bs + `ude00 <a href=` + bs + `"x` + bs + `">rastreio</a>"}`
	if string(raw) != want {
		t.Fatalf("encodeJSON = %s, want %s", raw, want)
	}
}

func hijackAndClose(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	hj, ok := w.(http.Hijacker)
	if !ok {
		t.Fatal("server does not support hijacking")
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}
