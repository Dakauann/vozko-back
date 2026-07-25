package asaas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/affiliate"
)

func newTestService(srv *httptest.Server) *AsaasService {
	return &AsaasService{
		apiToken: "test-token",
		endpoint: srv.URL,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

const goodWallet = "12345678-1234-1234-1234-123456789abc"

func TestIsWalletError(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`{"errors":[{"description":"walletId not found"}]}`, true},
		{`{"errors":[{"description":"Carteira invalida"}]}`, true},
		{`{"errors":[{"description":"subconta nao encontrada"}]}`, true},
		{`{"errors":[{"description":"invalid split config"}]}`, true},
		{`{"errors":[{"description":"something else entirely"}]}`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := isWalletError([]byte(c.body)); got != c.want {
			t.Errorf("isWalletError(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}

func TestValidateWalletID_BadFormat(t *testing.T) {
	svc := &AsaasService{endpoint: "http://unused", apiToken: "t", client: &http.Client{}}
	err := svc.ValidateWalletID("Alice", "11144477735", "not-a-uuid")
	if !errors.Is(err, ErrInvalidWalletID) {
		t.Fatalf("want ErrInvalidWalletID for bad format, got %v", err)
	}
	if err := svc.ValidateWalletID("Alice", "11144477735", "   "); !errors.Is(err, ErrInvalidWalletID) {
		t.Fatalf("want ErrInvalidWalletID for empty wallet, got %v", err)
	}
}

func TestValidateWalletID_MissingIdentity(t *testing.T) {
	svc := &AsaasService{endpoint: "http://unused", apiToken: "t", client: &http.Client{}}
	if err := svc.ValidateWalletID("", "doc", goodWallet); err == nil {
		t.Fatal("expected error for empty name")
	}
	if err := svc.ValidateWalletID("name", "", goodWallet); err == nil {
		t.Fatal("expected error for empty doc")
	}
}

type walletServerOpts struct {
	paymentStatus   int
	paymentBody     string
	deleteStatus    int
	wantDeleteCalls int
}

func newWalletServer(t *testing.T, opts *walletServerOpts) (*httptest.Server, *int) {
	t.Helper()
	deleteCalls := 0
	mux := http.NewServeMux()

	mux.HandleFunc("/customers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"cus_123","name":"x","cpfCnpj":"11144477735"}`)
	})

	mux.HandleFunc("/payments", func(w http.ResponseWriter, r *http.Request) {
		status := opts.paymentStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if opts.paymentBody != "" {
			_, _ = io.WriteString(w, opts.paymentBody)
			return
		}
		_, _ = io.WriteString(w, `{"id":"pay_abc","status":"PENDING"}`)
	})
	mux.HandleFunc("/payments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalls++
			status := opts.deleteStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &deleteCalls
}

func TestValidateWalletID_Success_DeletesPayment(t *testing.T) {
	srv, deletes := newWalletServer(t, &walletServerOpts{paymentStatus: http.StatusOK})
	svc := newTestService(srv)
	if err := svc.ValidateWalletID("Alice", "11144477735", goodWallet); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *deletes != 1 {
		t.Fatalf("expected 1 delete call, got %d", *deletes)
	}
}

func TestValidateWalletID_Success_Status201(t *testing.T) {
	srv, _ := newWalletServer(t, &walletServerOpts{paymentStatus: http.StatusCreated})
	svc := newTestService(srv)
	if err := svc.ValidateWalletID("Alice", "11144477735", goodWallet); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWalletID_Success_EmptyPaymentID(t *testing.T) {

	srv, deletes := newWalletServer(t, &walletServerOpts{
		paymentStatus: http.StatusOK,
		paymentBody:   `{"status":"PENDING"}`,
	})
	svc := newTestService(srv)
	if err := svc.ValidateWalletID("Alice", "11144477735", goodWallet); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *deletes != 0 {
		t.Fatalf("should not delete without id, got %d", *deletes)
	}
}

func TestValidateWalletID_Success_BadResponseBody(t *testing.T) {
	srv, _ := newWalletServer(t, &walletServerOpts{
		paymentStatus: http.StatusOK,
		paymentBody:   `not-json`,
	})
	svc := newTestService(srv)
	err := svc.ValidateWalletID("Alice", "11144477735", goodWallet)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWalletID_400_WalletKeyword(t *testing.T) {
	srv, _ := newWalletServer(t, &walletServerOpts{
		paymentStatus: http.StatusBadRequest,
		paymentBody:   `{"errors":[{"description":"walletId invalid"}]}`,
	})
	svc := newTestService(srv)
	err := svc.ValidateWalletID("Alice", "11144477735", goodWallet)
	if !errors.Is(err, ErrInvalidWalletID) {
		t.Fatalf("want ErrInvalidWalletID, got %v", err)
	}
}

func TestValidateWalletID_400_NonWallet(t *testing.T) {
	srv, _ := newWalletServer(t, &walletServerOpts{
		paymentStatus: http.StatusBadRequest,
		paymentBody:   `{"errors":[{"description":"something else"}]}`,
	})
	svc := newTestService(srv)
	err := svc.ValidateWalletID("Alice", "11144477735", goodWallet)
	if err == nil || errors.Is(err, ErrInvalidWalletID) {
		t.Fatalf("expected generic 400 error, got %v", err)
	}
}

func TestValidateWalletID_5xx(t *testing.T) {
	srv, _ := newWalletServer(t, &walletServerOpts{
		paymentStatus: http.StatusInternalServerError,
		paymentBody:   `boom`,
	})
	svc := newTestService(srv)
	err := svc.ValidateWalletID("Alice", "11144477735", goodWallet)
	if err == nil || errors.Is(err, ErrInvalidWalletID) {
		t.Fatalf("expected generic error, got %v", err)
	}
}

func TestValidateWalletID_TransportError(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	svc := newTestService(srv)
	err := svc.ValidateWalletID("Alice", "11144477735", goodWallet)
	if err == nil {
		t.Fatal("expected transport error")
	}
}

func TestDeletePayment_Success_200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	svc := newTestService(srv)
	if err := svc.DeletePayment("pay_ok"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestDeletePayment_Success_204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	svc := newTestService(srv)
	if err := svc.DeletePayment("pay_ok"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestDeletePayment_BadID(t *testing.T) {
	svc := &AsaasService{endpoint: "http://unused", apiToken: "t", client: &http.Client{}}
	if err := svc.DeletePayment(""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := svc.DeletePayment("bad id with space"); err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestDeletePayment_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	t.Cleanup(srv.Close)
	svc := newTestService(srv)
	err := svc.DeletePayment("pay_ok")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeletePayment_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	svc := newTestService(srv)
	if err := svc.DeletePayment("pay_ok"); err == nil {
		t.Fatal("expected transport error")
	}
}

type stubAsaasSvc struct {
	validateErr error
	called      bool
	gotName     string
	gotDoc      string
	gotWallet   string
}

func (s *stubAsaasSvc) GetOrCreateCustomer(name, cpf string) (*AsaasCustomer, error) {
	return &AsaasCustomer{ID: "cus_x"}, nil
}
func (s *stubAsaasSvc) CreatePayment(name, cpf string, p *AsaasPayment) (*AsaasPayment, error) {
	return nil, nil
}
func (s *stubAsaasSvc) GetPaymentQrCode(id string) (string, string, error) { return "", "", nil }
func (s *stubAsaasSvc) GetPayment(id string) (*AsaasPayment, error)        { return nil, nil }
func (s *stubAsaasSvc) RefundPayment(id string, amount int64, desc string) error {
	return nil
}
func (s *stubAsaasSvc) ValidateWalletID(name, doc, wallet string) error {
	s.called = true
	s.gotName, s.gotDoc, s.gotWallet = name, doc, wallet
	return s.validateErr
}
func (s *stubAsaasSvc) DeletePayment(id string) error { return nil }

func TestWalletValidatorAdapter_Success(t *testing.T) {
	stub := &stubAsaasSvc{}
	v := NewWalletValidator(stub)
	err := v.ValidateWallet(context.Background(), affiliate.WalletValidationInput{
		WalletID: "w", CustomerName: "n", CustomerDoc: "d",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !stub.called || stub.gotWallet != "w" || stub.gotName != "n" || stub.gotDoc != "d" {
		t.Fatalf("inputs not forwarded: %+v", stub)
	}
}

func TestWalletValidatorAdapter_InvalidWalletTranslated(t *testing.T) {
	stub := &stubAsaasSvc{validateErr: ErrInvalidWalletID}
	v := NewWalletValidator(stub)
	err := v.ValidateWallet(context.Background(), affiliate.WalletValidationInput{WalletID: "w"})
	if !errors.Is(err, affiliate.ErrInvalidAsaasWalletID) {
		t.Fatalf("want ErrInvalidAsaasWalletID, got %v", err)
	}
}

func TestWalletValidatorAdapter_OtherErrorWrapped(t *testing.T) {
	stub := &stubAsaasSvc{validateErr: fmt.Errorf("boom")}
	v := NewWalletValidator(stub)
	err := v.ValidateWallet(context.Background(), affiliate.WalletValidationInput{WalletID: "w"})
	if !errors.Is(err, affiliate.ErrWalletValidationFailed) {
		t.Fatalf("want wrapped ErrWalletValidationFailed, got %v", err)
	}
}

func TestWalletValidatorAdapter_NilReceiver(t *testing.T) {
	var v *walletValidatorAdapter
	err := v.ValidateWallet(context.Background(), affiliate.WalletValidationInput{WalletID: "w"})
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

func TestWalletValidatorAdapter_NilService(t *testing.T) {
	v := &walletValidatorAdapter{svc: nil}
	err := v.ValidateWallet(context.Background(), affiliate.WalletValidationInput{WalletID: "w"})
	if err == nil {
		t.Fatal("expected error for nil service")
	}
}

var _ = json.Marshal
