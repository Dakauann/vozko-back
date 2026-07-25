package asaas

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"vozko/brand"
)

type AsaasServiceUseCases interface {
	GetOrCreateCustomer(name string, cpf string) (*AsaasCustomer, error)

	CreatePayment(customerName string, customerCpf string, payment *AsaasPayment) (*AsaasPayment, error)
	GetPaymentQrCode(paymentID string) (qrCode string, copyPaste string, err error)

	GetPayment(paymentID string) (*AsaasPayment, error)
	RefundPayment(paymentID string, amount int64, description string) error

	ValidateWalletID(customerName string, customerDoc string, walletID string) error

	DeletePayment(paymentID string) error
}

type AsaasService struct {
	apiToken string
	endpoint string
	client   *http.Client
}

func NewAsaasService(apiToken, endpoint string) AsaasServiceUseCases {
	return &AsaasService{
		apiToken: apiToken,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

var asaasIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func validateAsaasID(id, label string) error {
	if id == "" || !asaasIDPattern.MatchString(id) {
		return fmt.Errorf("invalid %s: %q", label, id)
	}
	return nil
}

// ErrCustomerDocumentRequired guards against issuing a customer lookup with an empty cpfCnpj.
// Asaas treats `?cpfCnpj=` as a wildcard over document-less customers, so an empty document would
// resolve to an unrelated customer (mis-billing risk) instead of the intended one.
var ErrCustomerDocumentRequired = fmt.Errorf("cpfCnpj is required to resolve an asaas customer")

func (s *AsaasService) GetOrCreateCustomer(name string, cpf string) (*AsaasCustomer, error) {
	if strings.TrimSpace(cpf) == "" {
		return nil, ErrCustomerDocumentRequired
	}
	searchCustomerUrl := fmt.Sprintf("%s/customers?cpfCnpj=%s", s.endpoint, url.QueryEscape(cpf))

	searchReq, err := http.NewRequest(http.MethodGet, searchCustomerUrl, nil)
	if err != nil {
		return nil, err
	}
	searchReq.Header.Set("access_token", s.apiToken)

	searchRes, err := s.client.Do(searchReq)
	if err != nil {
		return nil, err
	}
	defer searchRes.Body.Close()

	if searchRes.StatusCode == http.StatusOK {
		var searchResponse struct {
			Data []AsaasCustomer `json:"data"`
		}
		if err := json.NewDecoder(searchRes.Body).Decode(&searchResponse); err != nil {
			return nil, err
		}

		if len(searchResponse.Data) > 0 {
			return &searchResponse.Data[0], nil
		}
	}

	createCustomerUrl := s.endpoint + "/customers"

	payload := map[string]string{
		"name":    name,
		"cpfCnpj": cpf,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	createReq, err := http.NewRequest(http.MethodPost, createCustomerUrl, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, err
	}
	createReq.Header.Set("access_token", s.apiToken)
	createReq.Header.Set("Content-Type", "application/json")

	createRes, err := s.client.Do(createReq)
	if err != nil {
		return nil, err
	}
	defer createRes.Body.Close()

	if createRes.StatusCode != http.StatusOK && createRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createRes.Body)
		return nil, fmt.Errorf("asaas create customer request failed with status %d: %s", createRes.StatusCode, string(body))
	}

	var customerResponse AsaasCustomer
	if err := json.NewDecoder(createRes.Body).Decode(&customerResponse); err != nil {
		return nil, err
	}

	if customerResponse.ID == "" {
		return nil, fmt.Errorf("customer ID is empty in response")
	}

	return &customerResponse, nil
}

func (s *AsaasService) CreatePayment(customerName string, customerCpf string, payment *AsaasPayment) (*AsaasPayment, error) {
	customer, err := s.GetOrCreateCustomer(customerName, customerCpf)
	if err != nil {
		return nil, err
	}

	payment.Customer = customer.ID
	createPaymentUrl := s.endpoint + "/payments"

	paymentData, err := json.Marshal(payment)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, createPaymentUrl, strings.NewReader(string(paymentData)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("access_token", s.apiToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("asaas create payment request failed with status %d: %s", res.StatusCode, string(body))
	}

	var createdPayment AsaasPayment
	if err := json.NewDecoder(res.Body).Decode(&createdPayment); err != nil {
		return nil, err
	}

	return &createdPayment, nil
}

func (s *AsaasService) GetPaymentQrCode(paymentID string) (qrCode string, copyPaste string, err error) {
	if err := validateAsaasID(paymentID, "payment ID"); err != nil {
		return "", "", err
	}
	qrCodeUrl := fmt.Sprintf("%s/payments/%s/pixQrCode", s.endpoint, paymentID)

	req, err := http.NewRequest(http.MethodGet, qrCodeUrl, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("access_token", s.apiToken)

	res, err := s.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("asaas get payment QR code request failed with status %d", res.StatusCode)
	}

	var qrCodeResponse struct {
		EncodedImage string `json:"encodedImage"`
		Payload      string `json:"payload"`
	}
	if err := json.NewDecoder(res.Body).Decode(&qrCodeResponse); err != nil {
		return "", "", err
	}

	return qrCodeResponse.EncodedImage, qrCodeResponse.Payload, nil
}

func (s *AsaasService) GetPayment(paymentID string) (*AsaasPayment, error) {
	if err := validateAsaasID(paymentID, "payment ID"); err != nil {
		return nil, err
	}
	getPaymentUrl := fmt.Sprintf("%s/payments/%s", s.endpoint, paymentID)

	req, err := http.NewRequest(http.MethodGet, getPaymentUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("access_token", s.apiToken)

	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asaas get payment request failed with status %d", res.StatusCode)
	}

	var payment AsaasPayment
	if err := json.NewDecoder(res.Body).Decode(&payment); err != nil {
		return nil, err
	}

	return &payment, nil
}

func (s *AsaasService) RefundPayment(paymentID string, amount int64, description string) error {
	if err := validateAsaasID(paymentID, "payment ID"); err != nil {
		return err
	}
	refundUrl := fmt.Sprintf("%s/payments/%s/refund", s.endpoint, paymentID)

	refundData := map[string]interface{}{
		"value":       amount,
		"description": description,
	}

	refundDataJSON, err := json.Marshal(refundData)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, refundUrl, strings.NewReader(string(refundDataJSON)))
	if err != nil {
		return err
	}
	req.Header.Set("access_token", s.apiToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("asaas refund payment request failed with status %d", res.StatusCode)
	}

	return nil
}

func (s *AsaasService) UpdatePayment(paymentID string, payment *AsaasPayment) (*AsaasPayment, error) {
	updatePaymentUrl := fmt.Sprintf("%s/payments/%s", s.endpoint, paymentID)

	paymentData, err := json.Marshal(payment)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPut, updatePaymentUrl, strings.NewReader(string(paymentData)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("access_token", s.apiToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asaas update payment request failed with status %d", res.StatusCode)
	}

	var updatedPayment AsaasPayment
	if err := json.NewDecoder(res.Body).Decode(&updatedPayment); err != nil {
		return nil, err
	}

	return &updatedPayment, nil
}

var ErrInvalidWalletID = fmt.Errorf("asaas rejected walletId")

var walletIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *AsaasService) ValidateWalletID(customerName string, customerDoc string, walletID string) error {
	walletID = strings.TrimSpace(walletID)
	if walletID == "" || !walletIDPattern.MatchString(walletID) {
		return ErrInvalidWalletID
	}
	if strings.TrimSpace(customerName) == "" || strings.TrimSpace(customerDoc) == "" {
		return fmt.Errorf("customer identity required for wallet validation")
	}

	customer, err := s.GetOrCreateCustomer(customerName, customerDoc)
	if err != nil {
		return fmt.Errorf("validate wallet: resolve customer: %w", err)
	}

	dueDate := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	payment := &AsaasPayment{
		Customer:    customer.ID,
		BillingType: "BOLETO",
		Value:       5.00,
		DueDate:     dueDate,
		Description: "Validação do split walletId para afiliado " + brand.Active().Name + " - cobrança de teste, favor ignorar",
		Split: []AsaasSplit{{
			WalletID:        walletID,
			PercentualValue: 1,
		}},
	}
	paymentData, err := json.Marshal(payment)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, s.endpoint+"/payments", strings.NewReader(string(paymentData)))
	if err != nil {
		return err
	}
	req.Header.Set("access_token", s.apiToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("validate wallet: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var created AsaasPayment
		if err := json.Unmarshal(body, &created); err != nil {
			return fmt.Errorf("validate wallet: decode response: %w", err)
		}
		if created.ID != "" {

			_ = s.DeletePayment(created.ID)
		}
		return nil
	case http.StatusBadRequest:
		if isWalletError(body) {
			return ErrInvalidWalletID
		}
		return fmt.Errorf("validate wallet: unexpected 400 response: %s", string(body))
	default:
		return fmt.Errorf("validate wallet: asaas returned status %d: %s", res.StatusCode, string(body))
	}
}

func isWalletError(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "wallet") ||
		strings.Contains(lower, "carteira") ||
		strings.Contains(lower, "subconta") ||
		strings.Contains(lower, "split")
}

func (s *AsaasService) DeletePayment(paymentID string) error {
	if err := validateAsaasID(paymentID, "payment ID"); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/payments/%s", s.endpoint, paymentID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("access_token", s.apiToken)

	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("asaas delete payment request failed with status %d: %s", res.StatusCode, string(body))
	}
	return nil
}
