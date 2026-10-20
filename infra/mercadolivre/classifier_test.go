package mercadolivre

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

func TestClassifierMirrorsThePredicates(t *testing.T) {
	var c mldomain.ErrorClassifier = Classifier{}
	auth := &Error{HTTPStatus: 401, Code: CodeInvalidToken}
	perm := &Error{HTTPStatus: 403, Code: CodePermissionPolicy}
	blocked := &Error{HTTPStatus: 403, Blocked: true, BlockedSubstatus: "blocked_by_mediation"}
	decode := &Error{Code: CodeDecode}
	if !c.IsAuth(auth) || c.IsAuth(perm) || !c.IsPermission(perm) {
		t.Fatal("auth and permission predicates disagree")
	}
	if sub, ok := c.Blocked(blocked); !ok || sub != "blocked_by_mediation" {
		t.Fatalf("blocked = %q %v", sub, ok)
	}
	if !c.IsDecode(decode) || c.IsDecode(auth) || c.IsDecode(errors.New("x")) {
		t.Fatal("decode predicate")
	}
	if !c.IsRetryable(&Error{Code: CodeTransport}) || c.IsRetryable(decode) {
		t.Fatal("retryable predicate")
	}
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"blocked 403":          {blocked, true},
		"rate limited 429":     {&Error{HTTPStatus: 429}, true},
		"local limit":          {&Error{Code: CodeLocalRateLimited}, true},
		"limiter down":         {&Error{Code: CodeLimiterUnavailable}, true},
		"server error":         {&Error{HTTPStatus: 500}, false},
		"timeout on a post":    {&Error{Code: CodeTransport, Ambiguous: true}, false},
		"accepted, unreadable": {&Error{HTTPStatus: 400, Ambiguous: true}, false},
		"foreign error":        {errors.New("x"), false},
	} {
		if got := c.IsDefiniteRefusal(tc.err); got != tc.want {
			t.Errorf("%s: definite refusal = %v, want %v", name, got, tc.want)
		}
	}
}

func TestPKCEProducesAVerifierAndItsChallenge(t *testing.T) {
	var p mldomain.PKCE = PKCE{}
	v, err := p.NewVerifier()
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Challenge(v)
	if err != nil || len(c) != 43 || c == v {
		t.Fatalf("challenge = %q, %v", c, err)
	}
	if _, err := p.Challenge("short"); err == nil {
		t.Fatal("a weak verifier was accepted")
	}
}

func TestMediaFetcherRefusesOversizeAndNonHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte(strings.Repeat("a", 100)))
	}))
	defer srv.Close()
	f := NewMediaFetcher(srv.Client())
	got, err := f.Fetch(context.Background(), srv.URL+"/a.pdf", 1000)
	if err != nil || len(got.Data) != 100 || got.ContentType != "application/pdf" {
		t.Fatalf("fetch = %+v, %v", got, err)
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/a.pdf", 10); !errors.Is(err, mldomain.ErrMediaTooLarge) {
		t.Fatalf("oversize = %v", err)
	}
	if _, err := f.Fetch(context.Background(), "file:///etc/passwd", 10); err == nil {
		t.Fatal("a non http url was fetched")
	}
}
