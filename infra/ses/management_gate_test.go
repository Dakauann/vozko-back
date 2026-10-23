package ses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ratelimit"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	sesv1 "github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"

	es "vozko/domain/emailsending"
)

type countingLimiter struct {
	mu    sync.Mutex
	lanes []es.ProviderLane
	err   error
}

func (l *countingLimiter) Await(_ context.Context, lane es.ProviderLane) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lanes = append(l.lanes, lane)
	return l.err
}

func (l *countingLimiter) awaited() []es.ProviderLane {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]es.ProviderLane(nil), l.lanes...)
}

type sesEndpoint struct {
	throttleLeft atomic.Int32
	requests     atomic.Int32
}

func (f *sesEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	if f.throttleLeft.Load() > 0 {
		f.throttleLeft.Add(-1)
		w.Header().Set("X-Amzn-ErrorType", "TooManyRequestsException")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Too Many Requests"}`))
		return
	}
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/v2/email/account"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ProductionAccessEnabled":true,"SendQuota":{"Max24HourSend":50000,"MaxSendRate":14,"SentLast24Hours":120}}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/email/outbound-emails"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MessageId":"ses-1"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<ListReceiptFiltersResponse xmlns="http://ses.amazonaws.com/doc/2010-12-01/"><ListReceiptFiltersResult><Filters/></ListReceiptFiltersResult><ResponseMetadata><RequestId>r-1</RequestId></ResponseMetadata></ListReceiptFiltersResponse>`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type noBackoff struct{}

func (noBackoff) BackoffDelay(int, error) (time.Duration, error) { return 0, nil }

func testClient(endpoint string, limiter es.ProviderCallLimiter, attempts int) *sesv2.Client {
	return sesv2.New(sesv2.Options{
		Region:       "sa-east-1",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
		Retryer: retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = attempts
			o.Backoff = noBackoff{}
			o.RateLimiter = ratelimit.None
		}),
		APIOptions: []func(*middleware.Stack) error{PaceManagementCalls(limiter)},
	})
}

func TestManagementCallsWaitForTheLimiterInTheLaneOfTheirContext(t *testing.T) {
	srv := httptest.NewServer(&sesEndpoint{})
	defer srv.Close()
	limiter := &countingLimiter{}
	client := testClient(srv.URL, limiter, 3)
	ctx := es.WithProviderLane(context.Background(), es.LaneProtect)
	if _, err := client.GetAccount(ctx, &sesv2.GetAccountInput{}); err != nil {
		t.Fatal(err)
	}
	if got := limiter.awaited(); len(got) != 1 || got[0] != es.LaneProtect {
		t.Fatalf("awaited = %v, want one protect wait", got)
	}
}

func TestSendEmailNeverWaitsForTheManagementLimiter(t *testing.T) {
	srv := httptest.NewServer(&sesEndpoint{})
	defer srv.Close()
	limiter := &countingLimiter{}
	client := testClient(srv.URL, limiter, 3)
	_, err := client.SendEmail(context.Background(), &sesv2.SendEmailInput{
		FromEmailAddress: aws.String("a@envios.test"),
		Destination:      &types.Destination{ToAddresses: []string{"b@example.com"}},
		Content:          &types.EmailContent{Raw: &types.RawMessage{Data: []byte("Subject: x\r\n\r\nx")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := limiter.awaited(); len(got) != 0 {
		t.Fatalf("SendEmail waited %v", got)
	}
}

func TestAThrottledCallIsRetriedThroughTheLimiterThenReturnsTheThrottle(t *testing.T) {
	cases := []struct {
		name      string
		throttles int32
		wantErr   bool
		waits     int
	}{
		{name: "recovers on the second attempt", throttles: 1, waits: 2},
		{name: "gives up after the attempts", throttles: 10, wantErr: true, waits: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &sesEndpoint{}
			fake.throttleLeft.Store(tc.throttles)
			srv := httptest.NewServer(fake)
			defer srv.Close()
			limiter := &countingLimiter{}
			client := testClient(srv.URL, limiter, 3)
			_, err := client.GetAccount(context.Background(), &sesv2.GetAccountInput{})
			if got := len(limiter.awaited()); got != tc.waits {
				t.Fatalf("limiter waits = %d, want one per attempt (%d)", got, tc.waits)
			}
			if !tc.wantErr {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var throttled *types.TooManyRequestsException
			if !errors.As(err, &throttled) {
				t.Fatalf("err = %v, want the throttle so callers retry later", err)
			}
		})
	}
}

func TestALimiterThatGivesUpStopsTheCallBeforeItLeaves(t *testing.T) {
	fake := &sesEndpoint{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	limiter := &countingLimiter{err: context.Canceled}
	client := testClient(srv.URL, limiter, 3)
	if _, err := client.GetAccount(context.Background(), &sesv2.GetAccountInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if fake.requests.Load() != 0 {
		t.Fatalf("requests = %d, want none", fake.requests.Load())
	}
}

func TestNewClientsPacesBothSESClients(t *testing.T) {
	srv := httptest.NewServer(&sesEndpoint{})
	defer srv.Close()
	limiter := &countingLimiter{}
	clients, err := NewClients(context.Background(), Credentials{Region: "sa-east-1", AccessKeyID: "AKID", SecretAccessKey: "SECRET", EndpointURL: srv.URL}, limiter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.SES.GetAccount(context.Background(), &sesv2.GetAccountInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := clients.Receipts.ListReceiptFilters(context.Background(), &sesv1.ListReceiptFiltersInput{}); err != nil {
		var apiErr smithy.APIError
		errors.As(err, &apiErr)
		t.Fatal(err, apiErr)
	}
	if got := limiter.awaited(); len(got) != 2 {
		t.Fatalf("awaited = %v, want both clients paced", got)
	}
}

func TestNewClientsRefusesAMissingLimiter(t *testing.T) {
	_, err := NewClients(context.Background(), Credentials{Region: "sa-east-1", AccessKeyID: "AKID", SecretAccessKey: "SECRET"}, nil)
	if !errors.Is(err, ErrIncompleteConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestPaceCallsGatesEveryOperationItSelects(t *testing.T) {
	srv := httptest.NewServer(&sesEndpoint{})
	defer srv.Close()
	limiter := &countingLimiter{}
	client := sesv2.New(sesv2.Options{
		Region:       "sa-east-1",
		BaseEndpoint: aws.String(srv.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
		APIOptions:   []func(*middleware.Stack) error{PaceCalls(limiter, func(string) bool { return true })},
	})
	ctx := es.WithProviderLane(context.Background(), es.LaneBackground)
	if _, err := client.GetAccount(ctx, &sesv2.GetAccountInput{}); err != nil {
		t.Fatal(err)
	}
	if got := limiter.awaited(); len(got) != 1 || got[0] != es.LaneBackground {
		t.Fatalf("awaited = %v, want the selected call paced in its lane", got)
	}
}

func TestNewClientsSharesItsAWSConfigForOtherServices(t *testing.T) {
	clients, err := NewClients(context.Background(), Credentials{Region: "sa-east-1", AccessKeyID: "AKID", SecretAccessKey: "SECRET"}, &countingLimiter{})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := clients.Config.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if clients.Config.Region != "sa-east-1" || creds.AccessKeyID != "AKID" {
		t.Fatalf("config = %s %s", clients.Config.Region, creds.AccessKeyID)
	}
}

func TestPaceCallsLeavesUnselectedOperationsAlone(t *testing.T) {
	srv := httptest.NewServer(&sesEndpoint{})
	defer srv.Close()
	limiter := &countingLimiter{}
	client := sesv2.New(sesv2.Options{
		Region:       "sa-east-1",
		BaseEndpoint: aws.String(srv.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
		APIOptions:   []func(*middleware.Stack) error{PaceCalls(limiter, func(string) bool { return false })},
	})
	if _, err := client.GetAccount(context.Background(), &sesv2.GetAccountInput{}); err != nil {
		t.Fatal(err)
	}
	if got := limiter.awaited(); len(got) != 0 {
		t.Fatalf("awaited = %v, want none", got)
	}
}
