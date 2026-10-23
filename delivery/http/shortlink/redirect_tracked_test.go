package shortlink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	shortlinkdomain "vozko/domain/shortlink"
)

type fixedResolve struct{ link *shortlinkdomain.ResolvedLink }

func (f fixedResolve) Execute(context.Context, string) (*shortlinkdomain.ResolvedLink, error) {
	return f.link, nil
}

type capturedClicks struct {
	messages []shortlinkdomain.ClickMessage
}

func (c *capturedClicks) Execute(_ context.Context, msg shortlinkdomain.ClickMessage) error {
	c.messages = append(c.messages, msg)
	return nil
}

func TestAForgedClickTokenStillOpensTheRealDestination(t *testing.T) {
	clicks := &capturedClicks{}
	h := &ShortLinkHandler{
		resolveUC: fixedResolve{link: &shortlinkdomain.ResolvedLink{
			State: shortlinkdomain.ResolveOK, ShortLinkID: "sl-1", WorkspaceID: "ws-1", Code: "Ab3x",
			TargetURL: "https://loja.com/promo", RedirectType: shortlinkdomain.RedirectTemporary,
		}},
		publishUC: clicks,
	}
	for _, token := range []string{"forged-token", "1.dHBsOmF0dGVtcHQtMQ", "s"} {
		rec := httptest.NewRecorder()
		r := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/r/Ab3x/"+token, nil), map[string]string{"code": "Ab3x", "token": token})
		h.RedirectTracked(rec, r)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://loja.com/promo" {
			t.Fatalf("token %q: status %d location %q", token, rec.Code, rec.Header().Get("Location"))
		}
	}
	if len(clicks.messages) != 3 {
		t.Fatalf("clicks = %d, the short link still counts the visit; the token only decides the button credit", len(clicks.messages))
	}
}
