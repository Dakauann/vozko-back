package formtools

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/report"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	report_usecase "vozko/usecases/report"
	workflow_usecase "vozko/usecases/workflow"
)

func requirementKeys(tool copilot.Tool) []string {
	needs, ok := tool.(interface {
		AlsoRequires() []workspace.PermissionEntry
	})
	if !ok {
		return nil
	}
	keys := []string{}
	for _, p := range needs.AlsoRequires() {
		keys = append(keys, p.Key())
	}
	return keys
}

func requires(tool copilot.Tool, keys ...string) bool {
	held := map[string]bool{}
	for _, key := range requirementKeys(tool) {
		held[key] = true
	}
	for _, key := range keys {
		if !held[key] {
			return false
		}
	}
	return len(keys) > 0
}

func TestEveryKnownRefusalHasAPortugueseExplanation(t *testing.T) {
	cases := map[error]string{
		wf.ErrForbidden:                               "permissão",
		wf.ErrFormNotFound:                            "list_whatsapp_forms",
		wf.ErrFormArchived:                            "arquivado",
		wf.ErrNameRequired:                            "nome",
		wf.ErrNameTooLong:                             fmt.Sprint(wf.MaxNameLength),
		wf.ErrNameTaken:                               "já existe",
		wf.ErrStaleDraft:                              "rascunho",
		wf.ErrDescriptionTooLong:                      fmt.Sprint(wf.MaxDescriptionLength),
		wf.ErrStarterUnknown:                          "lead_capture",
		wf.ErrNotOwner:                                "dono",
		wf.ErrProviderUnsupported:                     "WhatsApp oficial",
		wf.ErrNoEligibleAccount:                       "número do WhatsApp oficial",
		wf.ErrNoPublication:                           "publish_whatsapp_form",
		wf.ErrPublicationNotSendable:                  "Meta",
		wf.ErrOutsideWindow:                           "send_template",
		wf.ErrChannelUnsupported:                      "WhatsApp oficial",
		wf.ErrCopyInvalid:                             fmt.Sprint(conversation.WhatsAppFlowCTAMaxLen),
		wf.ErrListQueryInvalid:                        "AAAA-MM-DD",
		wf.ErrResponseNotFound:                        "resposta",
		wf.ErrWorkspaceRequired:                       "workspace",
		conversation.ErrWindowClosed:                  "send_template",
		conversation.ErrSendOutcomeUnknown:            "confira a conversa",
		workflow_usecase.ErrNoDefaultDealFunnel:       "funil",
		report.ErrNotAllowed:                          "exportar",
		report.ErrInvalidParams:                       "exportação",
		report_usecase.ErrNotConfigured:               "indisponível",
		fmt.Errorf("wrapped: %w", wf.ErrFormNotFound): "list_whatsapp_forms",
	}
	for err, want := range cases {
		res := failure(err)
		if res.Status == copilot.StatusOK || !strings.Contains(res.Message, want) {
			t.Errorf("%v: %+v", err, res)
		}
		if shared.HasLongDash(res.Message) {
			t.Errorf("%v: the message carries a long dash", err)
		}
	}
}

func TestARefusalOfPermissionIsDenied(t *testing.T) {
	for _, err := range []error{wf.ErrForbidden, report.ErrNotAllowed} {
		if res := failure(err); res.Status != copilot.StatusDenied {
			t.Errorf("%v: %+v", err, res)
		}
	}
}

func TestMetaRefusalsAndProblemsAreSpelledOut(t *testing.T) {
	rejected := failure(&wf.MetaRejection{Code: 100, Message: "Flow name is not unique"})
	if !strings.Contains(rejected.Message, "Flow name is not unique") || !strings.Contains(rejected.Message, "Meta") {
		t.Fatalf("rejected = %+v", rejected)
	}
	problems := failure(wf.Problems{{Screen: "s_a", Block: "b_b", Property: "label", Code: wf.ProblemTooLong, Limit: 20}})
	if !strings.Contains(problems.Message, "s_a") || !strings.Contains(problems.Message, "até 20") {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestAnUnknownFailureIsNotPassedOffAsSuccess(t *testing.T) {
	res := failure(errors.New("database is down"))
	if res.Status != copilot.StatusError || strings.Contains(res.Message, "database") {
		t.Fatalf("res = %+v", res)
	}
}

func TestEveryProblemCodeIsExplained(t *testing.T) {
	codes := []string{
		wf.ProblemRequired, wf.ProblemTooLong, wf.ProblemTooMany, wf.ProblemTooFew, wf.ProblemDuplicate, wf.ProblemInvalid,
		wf.ProblemNotAllowed, wf.ProblemUnknownTarget, wf.ProblemBackwardRoute, wf.ProblemUnreachable, wf.ProblemNoInput,
		wf.ProblemBindingUnknown, wf.ProblemBindingKind, wf.ProblemBindingSensitive, wf.ProblemBindingDuplicate,
		wf.ProblemBindingOptions, wf.ProblemMetaValidation,
	}
	for _, code := range codes {
		if _, ok := problemWords[code]; !ok {
			t.Errorf("%s has no explanation", code)
		}
	}
}
