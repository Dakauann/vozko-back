package workflowwebhook

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/domain/workflow"
	"vozko/pkg/codegolden"
)

const webhookErrorCodesAdvice = "update testdata/error_codes.golden, and also add the code to vozko-front src/lib/workflows/error-codes.ts and translate workflowsPage.errors.<code> in the pt, en, es and de catalogs"

func TestEveryWebhookConfigErrorWritesItsStableCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, webhookConfigRefusals, handleWebhookConfigError)
	responsetest.RequireStableCodes(t, webhookRequestRefusals)
}

func TestAnUnmappedWebhookConfigErrorStillWritesACode(t *testing.T) {
	rec := httptest.NewRecorder()
	handleWebhookConfigError(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError || responsetest.CodeOf(t, rec) != refusalWebhookConfigFailed.Code {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAWebhookTriggerRejectionCarriesItsReasonAsTheCode(t *testing.T) {
	rec := httptest.NewRecorder()
	handleWebhookTriggerError(rec, errors.New("boom"))
	if responsetest.CodeOf(t, rec) != workflow.WebhookReasonInternal {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := response.RefusalCodes(response.ErrorRefusalsOf(webhookConfigRefusals), webhookRequestRefusals)
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, webhookErrorCodesAdvice)
}
