package emailwebhooks

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

	"vozko/delivery/http/emailwebhooks/snstest"
)

const (
	eventsTopic = "arn:aws:sns:sa-east-1:123456789012:email-events"
	certURL     = "https://sns.sa-east-1.amazonaws.com/SimpleNotificationService-abc.pem"
)

func sign(t *testing.T, kp snstest.KeyPair, e *Envelope) {
	t.Helper()
	e.Signature = kp.Sign(t, CanonicalString(*e), e.SignatureVersion)
}

func notification(version string) Envelope {
	subject := "Amazon SES Email Event Notification"
	return Envelope{
		Type:             typeNotification,
		MessageID:        "22b80b92-fdea-4c2c-8f9d-bdfb0c7bf324",
		TopicArn:         eventsTopic,
		Subject:          &subject,
		Message:          `{"eventType":"Delivery","mail":{"messageId":"x"}}`,
		Timestamp:        "2026-10-21T12:00:00.000Z",
		SignatureVersion: version,
		SigningCertURL:   certURL,
	}
}

type fakeIntake struct {
	accepted []string
	err      error
}

func (f *fakeIntake) Accept(id, topic, message string) error {
	f.accepted = append(f.accepted, id+"|"+topic+"|"+message)
	return f.err
}

type fakeConfirmer struct{ urls []string }

func (f *fakeConfirmer) Confirm(_ context.Context, u string) error {
	f.urls = append(f.urls, u)
	return nil
}

type rig struct {
	router    *mux.Router
	fetcher   *snstest.Fetcher
	intake    *fakeIntake
	confirmer *fakeConfirmer
	kp        snstest.KeyPair
}

func newRig(t *testing.T) *rig {
	t.Helper()
	kp := snstest.NewKeyPair(t, time.Now().Add(24*time.Hour))
	fetcher := snstest.NewFetcher(certURL, kp.Cert)
	verifier, err := NewVerifier("sa-east-1", fetcher)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{router: mux.NewRouter(), fetcher: fetcher, intake: &fakeIntake{}, confirmer: &fakeConfirmer{}, kp: kp}
	RegisterPublicRoutes(r.router, NewHandler(Deps{
		Verifier: verifier, AllowedTopics: []string{eventsTopic}, Intake: r.intake, Confirmer: r.confirmer,
	}))
	return r
}

func (r *rig) post(t *testing.T, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

func (r *rig) postEnvelope(t *testing.T, e Envelope) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(e)
	return r.post(t, SNSPath, raw, nil)
}

func TestSNSNotificationsAreVerifiedThenQueued(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run("signature version "+version, func(t *testing.T) {
			r := newRig(t)
			e := notification(version)
			sign(t, r.kp, &e)
			rec := r.postEnvelope(t, e)
			if rec.Code != http.StatusOK || len(r.intake.accepted) != 1 {
				t.Fatalf("code = %d accepted = %v", rec.Code, r.intake.accepted)
			}
			if r.intake.accepted[0] != e.MessageID+"|"+eventsTopic+"|"+e.Message {
				t.Fatalf("accepted = %q", r.intake.accepted[0])
			}
		})
	}
	t.Run("without subject", func(t *testing.T) {
		r := newRig(t)
		e := notification("2")
		e.Subject = nil
		sign(t, r.kp, &e)
		if rec := r.postEnvelope(t, e); rec.Code != http.StatusOK {
			t.Fatalf("code = %d", rec.Code)
		}
	})
}

func TestSNSRefusals(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*Envelope)
		resign     bool
		want       int
		wantFetch  bool
		wantIntake bool
	}{
		{"tampered message", func(e *Envelope) { e.Message = `{"eventType":"Bounce"}` }, false, http.StatusUnauthorized, true, false},
		{"topic not allowed", func(e *Envelope) { e.TopicArn = "arn:aws:sns:sa-east-1:999999999999:other" }, true, http.StatusForbidden, false, false},
		{"cert from another region", func(e *Envelope) { e.SigningCertURL = "https://sns.us-east-1.amazonaws.com/x.pem" }, true, http.StatusUnauthorized, false, false},
		{"cert host suffix attack", func(e *Envelope) { e.SigningCertURL = "https://sns.sa-east-1.amazonaws.com.evil.test/x.pem" }, true, http.StatusUnauthorized, false, false},
		{"cert with userinfo", func(e *Envelope) { e.SigningCertURL = "https://evil@sns.sa-east-1.amazonaws.com/x.pem" }, true, http.StatusUnauthorized, false, false},
		{"cert with port", func(e *Envelope) { e.SigningCertURL = "https://sns.sa-east-1.amazonaws.com:8443/x.pem" }, true, http.StatusUnauthorized, false, false},
		{"cert over http", func(e *Envelope) { e.SigningCertURL = "http://sns.sa-east-1.amazonaws.com/x.pem" }, true, http.StatusUnauthorized, false, false},
		{"cert not pem", func(e *Envelope) { e.SigningCertURL = "https://sns.sa-east-1.amazonaws.com/x.txt" }, true, http.StatusUnauthorized, false, false},
		{"unknown version", func(e *Envelope) { e.SignatureVersion = "3" }, true, http.StatusUnauthorized, false, false},
		{"unknown type", func(e *Envelope) { e.Type = "Surprise" }, true, http.StatusBadRequest, false, false},
		{"garbage signature", func(e *Envelope) { e.Signature = "!!!" }, false, http.StatusUnauthorized, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			e := notification("1")
			sign(t, r.kp, &e)
			tc.mutate(&e)
			if tc.resign {
				sign(t, r.kp, &e)
			}
			rec := r.postEnvelope(t, e)
			if rec.Code != tc.want {
				t.Fatalf("code = %d, want %d", rec.Code, tc.want)
			}
			if (len(r.fetcher.Calls()) > 0) != tc.wantFetch {
				t.Fatalf("fetch calls = %v", r.fetcher.Calls())
			}
			if len(r.intake.accepted) != 0 {
				t.Fatal("nothing may be queued")
			}
		})
	}
}

func TestSNSRefusesExpiredCertificateAndForeignKey(t *testing.T) {
	r := newRig(t)
	expired := snstest.NewKeyPair(t, time.Now().Add(-time.Minute))
	r.fetcher.Serve(expired.Cert)
	e := notification("1")
	sign(t, expired, &e)
	if rec := r.postEnvelope(t, e); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired certificate = %d", rec.Code)
	}
	other := snstest.NewKeyPair(t, time.Now().Add(time.Hour))
	r.fetcher.Serve(r.kp.Cert)
	e = notification("2")
	sign(t, other, &e)
	if rec := r.postEnvelope(t, e); rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign key = %d", rec.Code)
	}
}

func TestSNSBodyLimitAndGarbage(t *testing.T) {
	r := newRig(t)
	if rec := r.post(t, SNSPath, []byte(strings.Repeat("a", maxSNSBody+1)), nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d", rec.Code)
	}
	if rec := r.post(t, SNSPath, []byte("nope"), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestSNSQueueFailureAsksForRetry(t *testing.T) {
	r := newRig(t)
	r.intake.err = errors.New("rabbit down")
	e := notification("2")
	sign(t, r.kp, &e)
	if rec := r.postEnvelope(t, e); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestSNSSubscriptionConfirmation(t *testing.T) {
	subscription := func(subscribeURL string) Envelope {
		return Envelope{
			Type:             typeSubscription,
			MessageID:        "165545c9-2a5c-472c-8df2-7ff2be2b3b1b",
			Token:            "2336412f37",
			TopicArn:         eventsTopic,
			Message:          "You have chosen to subscribe to the topic.",
			SubscribeURL:     subscribeURL,
			Timestamp:        "2026-10-21T12:00:00.000Z",
			SignatureVersion: "1",
			SigningCertURL:   certURL,
		}
	}
	t.Run("pinned host is confirmed", func(t *testing.T) {
		r := newRig(t)
		u := "https://sns.sa-east-1.amazonaws.com/?Action=ConfirmSubscription&TopicArn=" + eventsTopic + "&Token=2336412f37"
		e := subscription(u)
		sign(t, r.kp, &e)
		if rec := r.postEnvelope(t, e); rec.Code != http.StatusOK || len(r.confirmer.urls) != 1 || r.confirmer.urls[0] != u {
			t.Fatalf("code = %d urls = %v", rec.Code, r.confirmer.urls)
		}
		if len(r.intake.accepted) != 0 {
			t.Fatal("confirmations are not queued")
		}
	})
	t.Run("foreign subscribe url is refused", func(t *testing.T) {
		r := newRig(t)
		e := subscription("https://evil.test/?Action=ConfirmSubscription")
		sign(t, r.kp, &e)
		if rec := r.postEnvelope(t, e); rec.Code != http.StatusUnauthorized || len(r.confirmer.urls) != 0 {
			t.Fatalf("code = %d urls = %v", rec.Code, r.confirmer.urls)
		}
	})
	t.Run("unsigned confirmation is refused", func(t *testing.T) {
		r := newRig(t)
		e := subscription("https://sns.sa-east-1.amazonaws.com/?Action=ConfirmSubscription")
		sign(t, r.kp, &e)
		e.Token = "changed"
		if rec := r.postEnvelope(t, e); rec.Code != http.StatusUnauthorized || len(r.confirmer.urls) != 0 {
			t.Fatalf("code = %d urls = %v", rec.Code, r.confirmer.urls)
		}
	})
}

func TestTheDNSAssistantWebhookIsGone(t *testing.T) {
	r := newRig(t)
	if rec := r.post(t, "/webhooks/email/entri", []byte(`{"id":"e1"}`), nil); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestHTTPCertificateFetcherParsesAndCaches(t *testing.T) {
	kp := snstest.NewKeyPair(t, time.Now().Add(time.Hour))
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write(kp.PEM)
	}))
	defer srv.Close()
	fetcher := NewHTTPCertificateFetcher(srv.Client())
	for i := 0; i < 2; i++ {
		cert, err := fetcher.Fetch(context.Background(), srv.URL+"/cert.pem")
		if err != nil || !cert.Equal(kp.Cert) {
			t.Fatalf("cert = %v err = %v", cert, err)
		}
	}
	if hits != 1 {
		t.Fatalf("hits = %d, the certificate must be cached", hits)
	}
	if _, err := ParseCertificate([]byte("nope")); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestNewVerifierRejectsOddRegions(t *testing.T) {
	for _, region := range []string{"", "sa-east-1.evil", "a/b", "x:y"} {
		if _, err := NewVerifier(region, snstest.NewFetcher(certURL, nil)); err == nil {
			t.Errorf("region %q accepted", region)
		}
	}
}
