package whatsapptemplate

import (
	"path/filepath"
	"testing"

	"vozko/domain/whatsapp/template"
	"vozko/pkg/codegolden"
)

func TestTemplateErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := append(template.ErrorCodes(), codeTrackingUnavailable)
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes,
		"update testdata/error_codes.golden, and also add the code to vozko-front src/lib/whatsapp-templates/errors.ts and translate whatsappTemplates.errors.<code> in the pt, en, es and de catalogs")
}
