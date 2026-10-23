package googleaccount

import (
	"path/filepath"
	"testing"

	"vozko/pkg/codegolden"
)

const accountErrorCodesAdvice = "update testdata/error_codes.golden, and also translate googleAccount.errors.<code> in the vozko-front pt, en, es and de catalogs"

func TestGoogleAccountErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), RefusalCodes(), accountErrorCodesAdvice)
}
