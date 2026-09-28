package facebook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"vozko/domain/cache"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta/metatest"
)

type countingLimiter struct {
	allowed int
	used    int
}

func (l *countingLimiter) Allow(string) (bool, time.Duration, error) {
	l.used++
	return l.used <= l.allowed, time.Minute, nil
}

func profileServiceWith(t *testing.T, allowed int, respond func(r *http.Request) string) (fbdomain.ProfileSettingsService, *[]recordedCall) {
	t.Helper()
	calls := &[]recordedCall{}
	svc, err := NewProfileService(ProfileConfig{
		Graph: GraphConfig{HTTPClient: metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
			call := recordedCall{method: r.Method, path: r.URL.Path, query: map[string]string{}}
			for k := range r.URL.Query() {
				call.query[k] = r.URL.Query().Get(k)
			}
			if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
				_ = json.Unmarshal(raw, &call.body)
			}
			*calls = append(*calls, call)
			_, _ = w.Write([]byte(respond(r)))
		})},
		RateLimiterFactory: func(string, int, time.Duration) cache.RateLimiter { return &countingLimiter{allowed: allowed} },
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, calls
}

func TestProfileReadDecodesEveryField(t *testing.T) {
	svc, calls := profileServiceWith(t, 10, func(*http.Request) string {
		return `{"data":[{"greeting":[{"locale":"default","text":"Olá"}],"get_started":{"payload":"GET_STARTED"},
			"ice_breakers":[{"locale":"default","call_to_actions":[{"question":"Horário?","payload":"HOURS"}]}],
			"persistent_menu":[{"locale":"default","composer_input_disabled":true,"call_to_actions":[{"type":"web_url","title":"Site","url":"https://loja.example"}]}]}]}`
	})
	p, err := svc.Get(context.Background(), "PAGE", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/v25.0/me/messenger_profile" || (*calls)[0].query["fields"] == "" {
		t.Fatalf("call = %+v", (*calls)[0])
	}
	if p.Greeting[0].Text != "Olá" || p.GetStarted.Payload != "GET_STARTED" || p.IceBreakers[0].Items[0].Payload != "HOURS" ||
		!p.PersistentMenu[0].ComposerInputDisabled || p.PersistentMenu[0].Items[0].URL != "https://loja.example" {
		t.Fatalf("profile = %+v", p)
	}
}

func TestAnEmptyProfileReadsAsEmpty(t *testing.T) {
	svc, _ := profileServiceWith(t, 10, func(*http.Request) string { return `{"data":[]}` })
	p, err := svc.Get(context.Background(), "PAGE", "tok")
	if err != nil || p.GetStarted != nil || len(p.Greeting) != 0 {
		t.Fatalf("profile %+v %v", p, err)
	}
}

func TestProfileWriteUsesMessengerShapes(t *testing.T) {
	svc, calls := profileServiceWith(t, 10, func(*http.Request) string { return `{"result":"success"}` })
	err := svc.Set(context.Background(), "PAGE", "tok", fbdomain.MessengerProfile{
		GetStarted:  &fbdomain.GetStarted{Payload: "GET_STARTED"},
		IceBreakers: []fbdomain.IceBreakerSet{{Locale: "default", Items: []fbdomain.IceBreaker{{Question: "Q", Payload: "P"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := (*calls)[0].body
	breakers, _ := body["ice_breakers"].([]any)
	first, _ := breakers[0].(map[string]any)
	actions, _ := first["call_to_actions"].([]any)
	if (*calls)[0].method != http.MethodPost || len(actions) != 1 || body["get_started"] == nil || body["greeting"] != nil {
		t.Fatalf("body = %v", body)
	}
	if err := svc.Delete(context.Background(), "PAGE", "tok", []string{"greeting"}); err != nil {
		t.Fatal(err)
	}
	if (*calls)[1].method != http.MethodDelete {
		t.Fatalf("delete call = %+v", (*calls)[1])
	}
}

func TestProfileWritesAreThrottledPerPage(t *testing.T) {
	svc, calls := profileServiceWith(t, 1, func(*http.Request) string { return `{"result":"success"}` })
	ctx := context.Background()
	if err := svc.Set(ctx, "PAGE", "tok", fbdomain.MessengerProfile{}); err != nil {
		t.Fatal(err)
	}
	err := svc.Set(ctx, "PAGE", "tok", fbdomain.MessengerProfile{})
	if fbdomain.Classify(err) != fbdomain.FailureRetryable || len(*calls) != 1 {
		t.Fatalf("second write: %v after %d calls", err, len(*calls))
	}
}
