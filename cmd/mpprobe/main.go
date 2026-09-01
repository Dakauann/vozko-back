// Command mpprobe issues one real Mercado Pago charge with the exact payload the
// application sends, and prints the raw request and response.
//
// It exists because Mercado Pago reports most charge failures through an opaque
// wrapper ("fill and validate error list: communication_error"), which is impossible to
// diagnose from an application log alone. Running the same request in isolation shows
// the full response body and separates "our payload is wrong" from "the account is not
// set up for this".
//
// Usage:
//
//	go run ./cmd/mpprobe -amount 5 -email buyer@example.com -doc 11144477735
//
// The access token is read from MERCADOPAGO_ACCESS_TOKEN, and the notification URL from
// MERCADOPAGO_NOTIFICATION_URL, so it exercises the same configuration the server uses.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/infra/mercadopago"
)

func main() {
	var (
		amount   = flag.Float64("amount", 5, "charge amount in BRL")
		email    = flag.String("email", "", "payer email (REQUIRED; must not be the collector account's own email)")
		doc      = flag.String("doc", "11144477735", "payer CPF or CNPJ")
		name     = flag.String("name", "Teste Silva", "payer full name")
		method   = flag.String("method", "pix", "pix or bolbradesco")
		expiry   = flag.Duration("expires-in", 72*time.Hour, "time until the charge expires")
		baseURL  = flag.String("base-url", "", "override the API host (default https://api.mercadopago.com)")
		skipHook = flag.Bool("no-notification-url", false, "omit notification_url (isolates webhook URL validation failures)")

		createUser = flag.Bool("create-test-user", false,
			"create a sandbox test user and print its email, instead of issuing a charge")
		siteID = flag.String("site", "MLB", "country site for -create-test-user (MLB = Brazil)")
	)
	flag.Parse()

	token := strings.TrimSpace(os.Getenv("MERCADOPAGO_ACCESS_TOKEN"))
	if token == "" {
		fail("MERCADOPAGO_ACCESS_TOKEN is not set")
	}

	host := strings.TrimRight(strings.TrimSpace(*baseURL), "/")
	if host == "" {
		host = mercadopago.DefaultBaseURL
	}

	if *createUser {
		createTestUser(host, token, *siteID)
		return
	}

	if strings.TrimSpace(*email) == "" {
		fail("-email is required. Use a payer address that is NOT the collector account's own login,\n" +
			"       and with sandbox credentials use a test user's email.\n" +
			"       Run with -create-test-user to mint one and print its address.")
	}

	fmt.Printf("token      : %s… (%s)\n", safePrefix(token), tokenEnvironment(token))
	fmt.Printf("host       : %s\n", host)

	document := mercadopago.OnlyDigits(*doc)
	first, last := mercadopago.SplitName(*name)

	req := mercadopago.CreatePaymentRequest{
		TransactionAmount: *amount,
		PaymentMethodID:   *method,
		Description:       "Probe de integração Mercado Pago",
		ExternalReference: "probe:" + uuid.NewString(),
		DateOfExpiration:  mercadopago.FormatExpiration(time.Now().Add(*expiry), time.Now(), *method == mercadopago.PaymentMethodPix),
		Payer: mercadopago.PayerRequest{
			Email:     strings.TrimSpace(*email),
			FirstName: first,
			LastName:  last,
			Identification: &mercadopago.Identification{
				Type:   mercadopago.IdentificationTypeFor(document),
				Number: document,
			},
		},
	}
	if !*skipHook {
		req.NotificationURL = strings.TrimRight(strings.TrimSpace(os.Getenv("MERCADOPAGO_NOTIFICATION_URL")), "/")
	}
	if *method == mercadopago.PaymentMethodBoleto {
		req.Payer.Address = &mercadopago.PayerAddress{
			ZipCode: "01310100", StreetName: "Av. Paulista", StreetNumber: "1000",
			Neighborhood: "Bela Vista", City: "Sao Paulo", FederalUnit: "SP",
		}
	}

	body, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		fail("encode request: %v", err)
	}
	fmt.Printf("\n--- REQUEST POST %s/v1/payments ---\n%s\n", host, body)

	httpReq, err := http.NewRequest(http.MethodPost, host+"/v1/payments", bytes.NewReader(body))
	if err != nil {
		fail("build request: %v", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-Idempotency-Key", uuid.NewString())

	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(httpReq)
	if err != nil {
		fail("request failed: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	fmt.Printf("\n--- RESPONSE %d ---\n", res.StatusCode)
	printJSON(raw)

	if res.StatusCode >= 200 && res.StatusCode <= 299 {
		var p mercadopago.Payment
		if err := json.Unmarshal(raw, &p); err == nil {
			fmt.Printf("\nOK  id=%d status=%s/%s\n", p.ID, p.Status, p.StatusDetail)
			if qr := p.PointOfInteraction.TransactionData.QRCode; qr != "" {
				fmt.Printf("PIX copia-e-cola: %s\n", qr)
			}
			if url := p.TransactionDetails.ExternalResourceURL; url != "" {
				fmt.Printf("Boleto: %s\n", url)
			}
		}
		return
	}

	// Reuse the production error parsing so the hint shown here is exactly the hint the
	// server would log.
	var apiErr mercadopago.APIError
	_ = json.Unmarshal(raw, &apiErr)
	respErr := &mercadopago.ResponseError{
		StatusCode: res.StatusCode,
		Message:    apiErr.Message,
		ErrorCode:  apiErr.Error,
		Causes:     apiErr.Cause,
		Body:       string(raw),
	}
	fmt.Printf("\nFAILED: %s\n", respErr.Error())
	fmt.Println("\nChecklist for an opaque failure:")
	fmt.Println("  1. PIX key: the collector account must have one registered before it can issue PIX charges.")
	fmt.Println("  2. Payer identity: the payer email must not belong to the collector account itself.")
	fmt.Println("  3. Environment: a TEST- token needs a sandbox test-user payer; a production token needs a real one.")
	fmt.Println("  4. Re-run with -no-notification-url to rule out webhook URL validation.")
	fmt.Println("  5. Re-run with -method bolbradesco: if boleto succeeds while PIX fails, it is the PIX key.")
	os.Exit(1)
}

func printJSON(raw []byte) {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err == nil {
		fmt.Println(pretty.String())
		return
	}
	fmt.Println(string(raw))
}

func tokenEnvironment(token string) string {
	if strings.HasPrefix(token, "TEST-") {
		return "sandbox (TEST- prefix): payers must be sandbox test users"
	}
	return "production: charges are real"
}

func safePrefix(token string) string {
	if len(token) <= 12 {
		return "***"
	}
	return token[:12]
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}

// createTestUser mints a sandbox test user and prints its credentials.
//
// This exists because the dashboard shows a test account's username but not its email,
// while payer.email is exactly what a charge needs. POST /users/test_user returns the
// address explicitly, which removes the guesswork.
//
// Per Mercado Pago, test users are created with the PRODUCTION access token of the
// application, not a test one.
func createTestUser(host, token, siteID string) {
	body, err := json.Marshal(map[string]string{"site_id": siteID})
	if err != nil {
		fail("encode request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, host+"/users/test_user", bytes.NewReader(body))
	if err != nil {
		fail("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		fail("request failed: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	fmt.Printf("\n--- RESPONSE %d ---\n", res.StatusCode)
	printJSON(raw)

	if res.StatusCode < 200 || res.StatusCode > 299 {
		fmt.Println("\nTest users are created with the application's PRODUCTION access token.")
		fmt.Println("A 401/403 here usually means MERCADOPAGO_ACCESS_TOKEN currently holds a test token.")
		os.Exit(1)
	}

	var user struct {
		ID       int64  `json:"id"`
		Nickname string `json:"nickname"`
		Password string `json:"password"`
		Email    string `json:"email"`
		SiteID   string `json:"site_id"`
	}
	if err := json.Unmarshal(raw, &user); err != nil {
		fail("decode response: %v", err)
	}

	fmt.Printf("\nTest user created.\n")
	fmt.Printf("  id       : %d\n", user.ID)
	fmt.Printf("  nickname : %s\n", user.Nickname)
	fmt.Printf("  password : %s\n", user.Password)
	fmt.Printf("  email    : %s   <-- use this as the payer\n", user.Email)
	fmt.Printf("\nFor an end-to-end run through the app, put it in .env:\n")
	fmt.Printf("  MERCADOPAGO_SANDBOX_PAYER_EMAIL=%s\n", user.Email)
	fmt.Printf("(honoured only when APP_ENV=development)\n")
}
