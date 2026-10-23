package carriermessaging

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	cm "vozko/domain/carriermessaging"
	workspace_domain "vozko/domain/workspace"
)

func TestEveryProtectedRouteIsGuarded(t *testing.T) {
	router := mux.NewRouter()
	guards := map[string]string{}
	ac := func(res workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			guards[r.Method+" "+r.URL.Path] = string(res) + ":" + string(action)
		}
	}
	RegisterProtectedRoutes(router, &Handler{}, ac)
	registerRCSRoutes(router, &Handler{}, guardsOf(ac))
	registerSMSRoutes(router, &Handler{}, guardsOf(ac))
	carrier, inbox := string(workspace_domain.ResourceCarrierMessaging), string(workspace_domain.ResourceCarrierInboxes)
	cases := []struct {
		method, path, want string
	}{
		{http.MethodGet, "/rcs/senders", carrier + ":read"},
		{http.MethodPost, "/rcs/senders", carrier + ":configure"},
		{http.MethodGet, "/rcs/senders/s1", carrier + ":read"},
		{http.MethodPatch, "/rcs/senders/s1", carrier + ":configure"},
		{http.MethodDelete, "/rcs/senders/s1", carrier + ":configure"},
		{http.MethodPost, "/rcs/senders/s1/submit", carrier + ":configure"},
		{http.MethodPost, "/rcs/senders/s1/pause", carrier + ":configure"},
		{http.MethodPost, "/rcs/senders/s1/resume", carrier + ":configure"},
		{http.MethodPut, "/rcs/senders/s1/inbox", inbox + ":update"},
		{http.MethodPost, "/rcs/content/check", carrier + ":read"},
		{http.MethodPost, "/rcs/media", carrier + ":update"},
		{http.MethodGet, "/sms/routes", carrier + ":read"},
		{http.MethodPost, "/sms/routes", carrier + ":configure"},
		{http.MethodPatch, "/sms/routes/r1", carrier + ":configure"},
		{http.MethodDelete, "/sms/routes/r1", carrier + ":configure"},
		{http.MethodPost, "/sms/routes/r1/pause", carrier + ":configure"},
		{http.MethodPost, "/sms/routes/r1/resume", carrier + ":configure"},
		{http.MethodPut, "/sms/routes/r1/inbox", inbox + ":update"},
		{http.MethodPost, "/sms/text/check", carrier + ":read"},
		{http.MethodPost, "/sms/text/to-gsm", carrier + ":read"},
		{http.MethodGet, "/carrier/settings", carrier + ":read"},
		{http.MethodPut, "/carrier/settings", carrier + ":configure"},
		{http.MethodGet, "/carrier/suppressions", carrier + ":read"},
		{http.MethodPost, "/carrier/suppressions", carrier + ":configure"},
		{http.MethodDelete, "/carrier/suppressions/5511999990000", carrier + ":configure"},
		{http.MethodPost, "/carrier/test-sends", carrier + ":update"},
		{http.MethodGet, "/carrier/channels", carrier + ":read"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", tc.method, tc.path)
			continue
		}
		match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		if got := guards[tc.method+" "+tc.path]; got != tc.want {
			t.Errorf("%s %s guarded by %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestPublicRoutesAreTheWebhookAndTheLink(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, &Handler{}, &WebhookHandler{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/webhooks/carrier/cmcom/" + testToken + "/status"},
		{http.MethodGet, "/public/carrier/r/token"},
	} {
		var match mux.RouteMatch
		if !router.Match(httptest.NewRequest(tc.method, tc.path, nil), &match) {
			t.Errorf("%s %s is not registered", tc.method, tc.path)
		}
	}
}

func TestChannelRoutesWaitUntilTheChannelIsLaunched(t *testing.T) {
	pathsOf := map[cm.Channel][]string{
		cm.ChannelRCS: {"/rcs/senders", "/rcs/content/check", "/rcs/media"},
		cm.ChannelSMS: {"/sms/routes", "/sms/text/check", "/sms/text/to-gsm"},
	}
	adminOf := map[cm.Channel]string{cm.ChannelRCS: "/carrier/senders", cm.ChannelSMS: "/carrier/sms/routes"}
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, &Handler{}, func(_ workspace_domain.Resource, _ workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		return h
	})
	admin := mux.NewRouter()
	RegisterAdminRoutes(admin, &Handler{})
	mounted := func(r *mux.Router, path string) bool {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			var match mux.RouteMatch
			if r.Match(httptest.NewRequest(method, path, nil), &match) && match.MatchErr == nil {
				return true
			}
		}
		return false
	}
	for channel, paths := range pathsOf {
		for _, path := range paths {
			if mounted(router, path) != channel.Launched() {
				t.Errorf("%s mounted = %v while %s launched is %v", path, mounted(router, path), channel, channel.Launched())
			}
		}
		if mounted(admin, adminOf[channel]) != channel.Launched() {
			t.Errorf("admin %s mounted = %v while %s launched is %v", adminOf[channel], mounted(admin, adminOf[channel]), channel, channel.Launched())
		}
	}
	if !mounted(router, "/carrier/channels") {
		t.Error("the channel list stays mounted so the frontend can ask")
	}
}
