package tiktok

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	ttdomain "vozko/domain/tiktok"
)

func TestNewClientRefusesAnIncompleteConfiguration(t *testing.T) {
	limiters := &fakeLimiters{}
	base := Config{AppID: "a", AppSecret: "s", RedirectURI: testRedirectURI, AuthorizeURL: testAuthorizeURL, RateLimiterFactory: limiters.factory()}
	if _, err := NewClient(base); err != nil {
		t.Fatalf("complete config refused: %v", err)
	}
	noLimiter := base
	noLimiter.RateLimiterFactory = nil
	if _, err := NewClient(noLimiter); !errors.Is(err, errLimiterFactoryRequired) {
		t.Errorf("no limiter: %v", err)
	}
	noSecret := base
	noSecret.AppSecret = " "
	if _, err := NewClient(noSecret); !errors.Is(err, errAppCredentialsRequired) {
		t.Errorf("no secret: %v", err)
	}
	badAuthorize := base
	badAuthorize.AuthorizeURL = "https://evil.example.com/v2/auth/authorize"
	if _, err := NewClient(badAuthorize); !errors.Is(err, ErrAuthorizeURLInvalid) {
		t.Errorf("bad authorize url: %v", err)
	}
	badRedirect := base
	badRedirect.RedirectURI = "https://api.example.com/cb"
	if _, err := NewClient(badRedirect); !errors.Is(err, ErrRedirectURIInvalid) {
		t.Errorf("bad redirect: %v", err)
	}
}

func TestEnvelopeCodesBecomeStructuredErrors(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeCode(w, CodeInvalidToken, "Invalid access token")
	})
	c := newTestClient(t, srv.server.URL, nil)
	_, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/get/", family: FamilyProfile, creds: &testCreds})
	e, ok := asError(err)
	if !ok || e.Code != CodeInvalidToken || e.RequestID != "req-err" || !IsAuth(err) {
		t.Fatalf("err = %#v", err)
	}
	if srv.count() != 1 {
		t.Errorf("an auth error must not be retried, got %d calls", srv.count())
	}
}

func TestReadsRetryOnSystemErrors(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		if attempt == 1 {
			writeCode(w, CodeSystemError, "System error")
			return
		}
		writeOK(w, map[string]string{"ok": "yes"})
	})
	c := newTestClient(t, srv.server.URL, nil)
	data, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/get/", family: FamilyProfile, creds: &testCreds})
	if err != nil || !strings.Contains(string(data), "yes") {
		t.Fatalf("data %s err %v", data, err)
	}
	if srv.count() != 2 {
		t.Errorf("calls = %d, want 2", srv.count())
	}
}

func TestSendsNeverRetryASystemErrorAndReportItAmbiguous(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeCode(w, CodeSystemError, "System error")
	})
	c := newTestClient(t, srv.server.URL, nil)
	_, err := c.do(context.Background(), request{method: http.MethodPost, path: "/business/message/send/", family: FamilyMessageSend, creds: &testCreds, body: []byte(`{}`)})
	if !IsAmbiguous(err) || IsDefiniteRefusal(err) {
		t.Fatalf("err = %#v, want ambiguous", err)
	}
	if srv.count() != 1 {
		t.Errorf("calls = %d, a send must be attempted once", srv.count())
	}
}

func TestSendsRetryWhenTikTokThrottledBeforeActing(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		if attempt == 1 {
			writeCode(w, CodeRateLimited, "Requests made too frequently")
			return
		}
		writeOK(w, map[string]any{"message": map[string]string{"message_id": "m1"}})
	})
	c := newTestClient(t, srv.server.URL, nil)
	if _, err := c.do(context.Background(), request{method: http.MethodPost, path: "/business/message/send/", family: FamilyMessageSend, creds: &testCreds, body: []byte(`{}`)}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if srv.count() != 2 {
		t.Errorf("calls = %d, want 2", srv.count())
	}
}

func TestATransportErrorOnASendIsAmbiguousAndNotRetried(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijack unsupported")
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	})
	c := newTestClient(t, srv.server.URL, nil)
	_, err := c.do(context.Background(), request{method: http.MethodPost, path: "/business/message/send/", family: FamilyMessageSend, creds: &testCreds, body: []byte(`{}`)})
	if !IsAmbiguous(err) {
		t.Fatalf("err = %#v, want ambiguous", err)
	}
	if srv.count() != 1 {
		t.Errorf("calls = %d", srv.count())
	}
}

func TestStateSettingPostsMarkedRetrySafeAreRetried(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		if attempt == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeOK(w, map[string]any{})
	})
	c := newTestClient(t, srv.server.URL, nil)
	if _, err := c.do(context.Background(), request{method: http.MethodPost, path: "/business/comment/hide/", family: FamilyCommentHide, creds: &testCreds, body: []byte(`{}`), retrySafe: true}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if srv.count() != 2 {
		t.Errorf("calls = %d", srv.count())
	}
}

func TestALimiterFailureRefusesTheCallWithoutReachingTikTok(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) { writeOK(w, map[string]any{}) })
	limiters := &fakeLimiters{err: errors.New("redis down")}
	c := newTestClient(t, srv.server.URL, limiters)
	_, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/get/", family: FamilyProfile, creds: &testCreds})
	if e, ok := asError(err); !ok || e.Code != CodeLimiterUnavailable {
		t.Fatalf("err = %#v", err)
	}
	if !IsDefiniteRefusal(err) {
		t.Error("a limiter refusal means nothing reached TikTok")
	}
	if srv.count() != 0 {
		t.Errorf("TikTok was called %d times despite the limiter failure", srv.count())
	}
}

func TestALongLocalLimitIsARefusalNotAWait(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) { writeOK(w, map[string]any{}) })
	limiters := &fakeLimiters{refuse: FamilyCommentList.limiterPrefix()}
	c := newTestClient(t, srv.server.URL, limiters)
	_, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/comment/list/", family: FamilyCommentList, creds: &testCreds})
	if !IsRateLimited(err) || srv.count() != 0 {
		t.Fatalf("err = %v calls = %d", err, srv.count())
	}
}

func TestBackgroundCallsAlsoConsumeTheBackgroundBudget(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) { writeOK(w, map[string]any{}) })
	limiters := &fakeLimiters{}
	c := newTestClient(t, srv.server.URL, limiters)
	ctx := ttdomain.WithBackground(context.Background())
	if _, err := c.do(ctx, request{method: http.MethodGet, path: "/business/comment/list/", family: FamilyCommentList, creds: &testCreds}); err != nil {
		t.Fatalf("err = %v", err)
	}
	got := strings.Join(limiters.prefixesCalled(), ",")
	if got != "tt_app,tt_comment_list,tt_bg_comment_list" {
		t.Fatalf("limiters = %s", got)
	}
	if limiters.budgets["tt_comment_list"] != AccountsAPIPerMinute || limiters.budgets["tt_bg_comment_list"] != 30 {
		t.Errorf("budgets = %v", limiters.budgets)
	}
}

func TestAccountCallsCarryTheTokenAndAppCallsDoNot(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) { writeOK(w, map[string]any{}) })
	c := newTestClient(t, srv.server.URL, nil)
	if _, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/get/", family: FamilyProfile, creds: &testCreds}); err != nil {
		t.Fatal(err)
	}
	if srv.last().Header.Get(headerAccessToken) != testCreds.AccessToken {
		t.Error("account call without the access token")
	}
	if _, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/webhook/list/", family: FamilyWebhookConfig}); err != nil {
		t.Fatal(err)
	}
	if srv.last().Header.Get(headerAccessToken) != "" {
		t.Error("app call carried an access token")
	}
}

func TestCallsWithoutUsableCredentialsAreRefusedLocally(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) { writeOK(w, map[string]any{}) })
	c := newTestClient(t, srv.server.URL, nil)
	for _, creds := range []ttdomain.Credentials{{OpenID: "x"}, {AccessToken: "t"}, {OpenID: "a b", AccessToken: "t"}} {
		creds := creds
		if _, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/get/", family: FamilyProfile, creds: &creds}); !errors.Is(err, ttdomain.ErrNeedsReconnect) {
			t.Errorf("creds %+v: err = %v", creds, err)
		}
	}
	if srv.count() != 0 {
		t.Errorf("TikTok called %d times", srv.count())
	}
}

func TestOversizedResponsesAreRefused(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxJSONBytes+10)))
	})
	c := newTestClient(t, srv.server.URL, nil)
	_, err := c.do(context.Background(), request{method: http.MethodGet, path: "/business/get/", family: FamilyProfile, creds: &testCreds})
	if e, ok := asError(err); !ok || e.Code != CodeResponseTooLarge {
		t.Fatalf("err = %#v", err)
	}
}

func TestHTTPErrorsKeepTheEnvelopeCodeWhenPresent(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":40001,"message":"No permission","request_id":"r"}`))
	})
	c := newTestClient(t, srv.server.URL, nil)
	_, err := c.do(context.Background(), request{method: http.MethodPost, path: "/business/comment/reply/create/", family: FamilyCommentReply, creds: &testCreds, body: []byte(`{}`)})
	if !IsPermission(err) || !IsDefiniteRefusal(err) {
		t.Fatalf("err = %#v", err)
	}
}

func TestAuthorizeURLAddsStateAndForcesConsent(t *testing.T) {
	c := newTestClient(t, "http://unused", nil)
	got, err := c.AuthorizeURL("signed.state")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(got, "https://www.tiktok.com/v2/auth/authorize?", "state=signed.state", "disable_auto_auth=1", "client_key=7043848340043202562") {
		t.Fatalf("url = %s", got)
	}
	if _, err := c.AuthorizeURL(" "); !errors.Is(err, ErrStateRequired) {
		t.Errorf("empty state: %v", err)
	}
}

func TestParseAuthorizeURL(t *testing.T) {
	refused := []string{
		"", "http://www.tiktok.com/v2/auth/authorize?x=1", "https://tiktok.com/v2/auth/authorize",
		"https://www.tiktok.com.evil.com/v2/auth/authorize", "https://www.tiktok.com/v1/auth/authorize",
		"https://www.tiktok.com/v2/auth/authorize?state=fixed", "https://www.tiktok.com/v2/auth/authorize#frag",
		"https://www.tiktok.com/v2/auth/authorize?disable_auto_auth=0",
	}
	for _, raw := range refused {
		if _, err := ParseAuthorizeURL(raw); !errors.Is(err, ErrAuthorizeURLInvalid) {
			t.Errorf("ParseAuthorizeURL(%q) err = %v", raw, err)
		}
	}
	if _, err := ParseAuthorizeURL(testAuthorizeURL); err != nil {
		t.Errorf("valid url refused: %v", err)
	}
}

func TestValidateRedirectURI(t *testing.T) {
	refused := []string{
		"", "http://api.example.com/oauth/tiktok/callback/", "https://api.example.com/oauth/tiktok/callback",
		"https://api.example.com:8443/oauth/tiktok/callback/", "https://api.example.com/oauth/tiktok/callback/?x=1",
		"https://api.example.com/oauth/tiktok/callback/#x", "https://api.example.com/" + strings.Repeat("a", 520) + "/",
	}
	for _, raw := range refused {
		if err := ValidateRedirectURI(raw); !errors.Is(err, ErrRedirectURIInvalid) {
			t.Errorf("ValidateRedirectURI(%q) err = %v", raw, err)
		}
	}
	if err := ValidateRedirectURI(testRedirectURI); err != nil {
		t.Errorf("valid redirect refused: %v", err)
	}
}

func TestExchangeCodeSendsTheAppCredentialsAndParsesTheGrant(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{
			"access_token": "act.1", "expires_in": 86400, "open_id": "_000abc", "refresh_token": "rft.1",
			"refresh_token_expires_in": 31536000, "scope": "user.info.basic,video.list,comment.list", "token_type": "Bearer",
		})
	})
	c := newTestClient(t, srv.server.URL, nil)
	g, err := c.ExchangeCode(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if g.OpenID != "_000abc" || g.AccessToken != "act.1" || g.RefreshToken != "rft.1" || g.ExpiresIn.Hours() != 24 || len(g.Scopes) != 3 {
		t.Errorf("grant = %+v", g)
	}
	body := decodeBody(t, srv.last())
	if body["client_id"] != "7043848340043202562" || body["client_secret"] != "app-secret" || body["grant_type"] != "authorization_code" || body["auth_code"] != "code-1" || body["redirect_uri"] != testRedirectURI {
		t.Errorf("body = %v", body)
	}
	if srv.last().Path != "/tt_user/oauth2/token/" {
		t.Errorf("path = %s", srv.last().Path)
	}
}

func TestExchangeCodeRefusesAGrantWithoutARefreshToken(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"access_token": "act.1", "expires_in": 86400, "open_id": "_000abc"})
	})
	c := newTestClient(t, srv.server.URL, nil)
	if _, err := c.ExchangeCode(context.Background(), "code"); !errors.Is(err, ttdomain.ErrGrantIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefreshAndTokenInfo(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		switch r.Path {
		case "/tt_user/oauth2/refresh_token/":
			writeOK(w, map[string]any{"access_token": "act.2", "expires_in": 86400, "open_id": "_000abc", "refresh_token": "rft.2", "refresh_token_expires_in": 28484364, "scope": "video.list"})
		case "/tt_user/token_info/get/":
			writeOK(w, map[string]any{"app_id": "7043848340043202562", "creator_id": "_000abc", "scope": "video.list,comment.list"})
		}
	})
	c := newTestClient(t, srv.server.URL, nil)
	g, err := c.Refresh(context.Background(), "rft.1")
	if err != nil || g.RefreshToken != "rft.2" {
		t.Fatalf("refresh = %+v %v", g, err)
	}
	if b := decodeBody(t, srv.last()); b["grant_type"] != "refresh_token" || b["refresh_token"] != "rft.1" {
		t.Errorf("refresh body = %v", b)
	}
	info, err := c.TokenInfo(context.Background(), "act.2")
	if err != nil || info.OpenID != "_000abc" || len(info.Scopes) != 2 {
		t.Fatalf("token info = %+v %v", info, err)
	}
	if b := decodeBody(t, srv.last()); b["app_id"] != "7043848340043202562" || b["access_token"] != "act.2" {
		t.Errorf("token info body = %v", b)
	}
}
