package workflowsecrethttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/pkg/codegolden"
)

const secretErrorCodesAdvice = "update testdata/error_codes.golden, and also add the code to vozko-front src/lib/workflows/error-codes.ts and translate workflowSecretsPage.errors.<code> in the pt, en, es and de catalogs"

func TestEverySecretErrorWritesItsStableCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, secretRefusals, writeDomainError)
	responsetest.RequireStableCodes(t, secretRequestRefusals)
}

func TestAnUnexpectedSecretErrorStillWritesACode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeDomainError(rec, errors.New("vault down"))
	if rec.Code != http.StatusInternalServerError || responsetest.CodeOf(t, rec) != refusalSecretFailed.Code {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestSecretErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := response.RefusalCodes(response.ErrorRefusalsOf(secretRefusals), secretRequestRefusals)
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, secretErrorCodesAdvice)
}
