package marketing

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

func TestGraphErrorsBecomeRemoteErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   advertising.RemoteError
	}{
		{
			name:   "expired token",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"Session has expired","type":"OAuthException","code":190,"error_subcode":463}}`,
			want:   advertising.RemoteError{Kind: advertising.FailureReauth, Code: 190, Subcode: 463, Message: "Session has expired"},
		},
		{
			name:   "invalid parameter with user message",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"Invalid parameter","code":100,"error_subcode":1885183,"error_user_title":"Orçamento baixo","error_user_msg":"O orçamento diário mínimo é R$ 5,00"}}`,
			want:   advertising.RemoteError{Kind: advertising.FailureRejected, Code: 100, Subcode: 1885183, Message: "Invalid parameter", UserTitle: "Orçamento baixo", UserMessage: "O orçamento diário mínimo é R$ 5,00"},
		},
		{
			name:   "permission",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"Missing ads_management","code":200}}`,
			want:   advertising.RemoteError{Kind: advertising.FailurePermission, Code: 200, Message: "Missing ads_management"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := gatewayWith(t, func(recordedCall) (int, string) { return tt.status, tt.body })
			_, err := g.CreateCampaign(context.Background(), "tok", "9", advertising.CampaignSpec{Name: "x"})
			var remote *advertising.RemoteError
			if !errors.As(err, &remote) {
				t.Fatalf("err = %v", err)
			}
			if *remote != tt.want {
				t.Fatalf("remote = %+v, want %+v", *remote, tt.want)
			}
		})
	}
}

func TestRateLimitIsRetryable(t *testing.T) {
	g, calls := gatewayWith(t, func(recordedCall) (int, string) {
		return http.StatusBadRequest, `{"error":{"message":"Application request limit reached","code":4}}`
	})
	_, err := g.GetAdAccount(context.Background(), "tok", "9")
	if advertising.Classify(err) != advertising.FailureRetryable || len(*calls) != 2 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset")
}

func TestTransportFailureIsUnknownButKeepsBothErrors(t *testing.T) {
	g, err := newGateway(Config{HTTPClient: &http.Client{Transport: failingTransport{}}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.CreateCampaign(context.Background(), "tok", "9", advertising.CampaignSpec{Name: "x"})
	var remote *advertising.RemoteError
	var request *meta.RequestError
	if !errors.As(err, &remote) || remote.Kind != advertising.FailureUnknown || !errors.As(err, &request) {
		t.Fatalf("err = %v", err)
	}
}

func TestContextErrorsPassThrough(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.ListPages(ctx, "tok")
	var remote *advertising.RemoteError
	if !errors.Is(err, context.Canceled) || errors.As(err, &remote) {
		t.Fatalf("err = %v", err)
	}
}

func TestFailureOfUndecodedError(t *testing.T) {
	if got := failureOf(&meta.Error{HTTPStatus: http.StatusBadRequest, Message: "<html>"}); got != advertising.FailureUnknown {
		t.Fatalf("failure = %s", got)
	}
	if got := failureOf(&meta.Error{HTTPStatus: http.StatusBadGateway}); got != advertising.FailureRetryable {
		t.Fatalf("failure = %s", got)
	}
}
