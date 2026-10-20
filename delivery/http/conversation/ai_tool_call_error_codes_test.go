package conversation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/pkg/codegolden"
	aa_usecase "vozko/usecases/ai_attendance"
)

const toolCallErrorCodesAdvice = "update testdata/ai_tool_call_error_codes.golden, and also add the code to vozko-front src/lib/workflows/error-codes.ts and translate workflowsPage.errors.<code> in the pt, en, es and de catalogs"

func TestEveryToolCallRefusalWritesItsStableCode(t *testing.T) {
	responsetest.RequireStableCodes(t, toolCallRefusals)
	tests := []struct {
		name string
		h    func() *ConversationHandler
		r    *http.Request
		want response.Refusal
	}{
		{"no tool call registry", func() *ConversationHandler { return &ConversationHandler{} }, toolCallsRequest("user"), refusalToolCallsUnavailable},
		{"a hidden conversation", withToolCalls(aa_usecase.ErrConversationNotVisible), toolCallsRequest("user"), refusalToolCallConversationHidden},
		{"an unreadable list", withToolCalls(errors.New("db down")), toolCallsRequest("user"), refusalToolCallsFailed},
		{"no caller", withToolCalls(nil), httptest.NewRequest(http.MethodGet, "/conversations/whatsapp/e/ai-tool-calls", nil), refusalToolCallsUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.h().ListAIToolCalls(rec, tc.r)
			if rec.Code != tc.want.Status || responsetest.CodeOf(t, rec) != tc.want.Code {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func withToolCalls(err error) func() *ConversationHandler {
	return func() *ConversationHandler {
		h := &ConversationHandler{}
		h.SetToolCalls(&stubToolCalls{err: err})
		return h
	}
}

func TestToolCallErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codegolden.Compare(t, filepath.Join("testdata", "ai_tool_call_error_codes.golden"), response.RefusalCodes(toolCallRefusals), toolCallErrorCodesAdvice)
}
