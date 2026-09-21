package template

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorCode_MapsSentinelsToStableCodes(t *testing.T) {
	assert.Equal(t, CodeBodyVariableAtStart, ErrorCode(ErrBodyVariableAtStart))
	assert.Equal(t, CodeNameInvalidChars, ErrorCode(ErrTemplateNameInvalidChars))
	assert.Equal(t, CodeInvalidCategory, ErrorCode(ErrInvalidCategory))
}

func TestErrorCode_WalksTheWrapChain(t *testing.T) {
	wrapped := fmt.Errorf("validating components: %w", ErrBodyTextTooLong)
	assert.Equal(t, CodeBodyTextTooLong, ErrorCode(wrapped))
	assert.True(t, IsValidationError(wrapped))
}

func TestErrorCode_UnknownErrorIsNamedNotEmpty(t *testing.T) {
	assert.Equal(t, CodeUnknown, ErrorCode(errors.New("database is down")))
	assert.Equal(t, "", ErrorCode(nil), "no error means no code")
	assert.False(t, IsValidationError(errors.New("database is down")))
	assert.False(t, IsValidationError(nil))
}

func TestKnownErrorCodes_IncludesTheProviderAndUnknownCodes(t *testing.T) {
	codes := KnownErrorCodes()
	assert.Contains(t, codes, CodeUnknown)
	assert.Contains(t, codes, CodeProviderRejected)
	assert.Contains(t, codes, CodeProviderUnavailable)
	assert.Contains(t, codes, CodeBodyNeedsExample)
}

func TestKnownErrorCodes_AreUniqueAndNonEmpty(t *testing.T) {
	seen := map[string]bool{}
	for _, code := range KnownErrorCodes() {
		assert.NotEmpty(t, code)
		assert.False(t, seen[code], "duplicate code %q — the UI would map two failures to one sentence", code)
		seen[code] = true
	}
}

func TestEverySentinelHasACode(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	require.NoError(t, err)

	var missing []string
	for _, pkg := range pkgs {
		if strings.HasSuffix(pkg.Name, "_test") {
			continue
		}
		for path, file := range pkg.Files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for i, name := range spec.Names {
					if !strings.HasPrefix(name.Name, "Err") || !name.IsExported() {
						continue
					}
					if i >= len(spec.Values) || !isErrorsNewCall(spec.Values[i]) {
						continue
					}
					if !codedSentinelNames[name.Name] {
						missing = append(missing, name.Name)
					}
				}
				return true
			})
		}
	}

	assert.Empty(t, missing,
		"these sentinels have no entry in errorCodes, so they reach the UI as raw English: %v", missing)
}

func isErrorsNewCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "errors" && sel.Sel.Name == "New"
}

var codedSentinelNames = map[string]bool{
	"ErrTemplateNotFound": true, "ErrTemplateAlreadyExists": true,
	"ErrExternalIDRequired": true, "ErrTemplateCategoryUnavailable": true,
	"ErrHeaderMediaURLNotApplicable": true, "ErrTemplateNameRequired": true,
	"ErrTemplateNameInvalidChars": true, "ErrTemplateNameMustStartLetter": true,
	"ErrTemplateNameTooLong": true, "ErrHeaderTextTooLong": true,
	"ErrHeaderTextTooManyVariables": true, "ErrHeaderFormatRequired": true,
	"ErrHeaderMediaNeedsHandle": true, "ErrBodyTextTooLong": true,
	"ErrBodyVariableAtStart": true, "ErrBodyVariableAtEnd": true,
	"ErrBodyConsecutiveVariables": true, "ErrBodyNeedsExample": true,
	"ErrFooterTextTooLong": true, "ErrFooterHasVariables": true,
	"ErrTooManyButtons": true, "ErrButtonTextTooLong": true,
	"ErrButtonTextRequired": true, "ErrButtonURLRequired": true,
	"ErrButtonPhoneRequired": true, "ErrButtonsNotGrouped": true,
	"ErrURLButtonVariableNotEnd": true, "ErrURLButtonTooManyVars": true,
	"ErrCopyCodeNeedsExample": true, "ErrCallPermissionWithButtons": true,
	"ErrMultipleCallPermissionRequests": true, "ErrMixedParameterStyles": true,
	"ErrInvalidComponentType": true, "ErrInvalidHeaderFormat": true,
	"ErrInvalidButtonType": true, "ErrInvalidCategory": true,
	"ErrWorkspaceRequired": true, "ErrIdempotencyKeyRequired": true,
	"ErrSendInProgress": true, "ErrTemplatePhoneMismatch": true,
	"ErrPricingUnavailable": true, "ErrTemplateNotSendable": true,
	"ErrBillingNotConfigured": true, "ErrSendAttemptConflict": true,
	"ErrOTPTypeRequired": true, "ErrInvalidOTPType": true,
	"ErrMultipleOTPButtons": true, "ErrOTPButtonNotAuthentication": true,
	"ErrOTPTypeUnsupported":           true,
	"ErrAuthenticationNeedsOTPButton": true, "ErrCodeExpirationOutOfRange": true,
	"ErrAuthenticationCodeRequired": true,
	"ErrAuthenticationNoHeader":     true, "ErrAuthenticationBodyNotEditable": true,
	"ErrAuthenticationFooterNotEditable": true, "ErrAuthenticationCodeTooLong": true,
}

func TestCodedSentinelNamesMatchesTheTable(t *testing.T) {
	assert.Len(t, codedSentinelNames, len(errorCodes),
		"the AST guard's name list and errorCodes disagree on how many sentinels are coded")
}
