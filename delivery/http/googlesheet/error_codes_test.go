package googlesheet

import (
	"fmt"
	"path/filepath"
	"testing"

	"vozko/domain/googlesheet"
	"vozko/pkg/codegolden"
)

const sheetGoldenAdvice = "update testdata/%s, and also translate googleSheets.%s.<code> in the vozko-front pt, en, es and de catalogs"

func TestSheetErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := append(append(RefusalCodes(), googlesheet.Codes()...), googlesheet.RowErrorCodes()...)
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, advice("error_codes.golden", "errors"))
}

func TestAttentionReasonsMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	var reasons []string
	for _, reason := range googlesheet.AttentionReasons() {
		reasons = append(reasons, string(reason))
	}
	codegolden.Compare(t, filepath.Join("testdata", "attention_reasons.golden"), reasons, advice("attention_reasons.golden", "reasons"))
}

func advice(golden, scope string) string {
	return fmt.Sprintf(sheetGoldenAdvice, golden, scope)
}
