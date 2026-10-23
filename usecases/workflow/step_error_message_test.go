package workflow_usecase

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

func TestAFailedStepShowsTheCodedMessageAndKeepsTheTechnicalTextInItsChain(t *testing.T) {
	refusal := fmt.Errorf("send text: %w", fmt.Errorf("%w (no customer message in the last 24 h). %s", conversation.ErrWindowClosed, workflow.WindowClosedFix))

	row := stepRow("run-1", step{nodeID: "ola", status: workflow.LogStatusFailed, err: refusal, trace: &workflow.StepTrace{}})

	if row.Error != workflow.WindowClosedFix {
		t.Fatalf("error = %q, want only the coded message", row.Error)
	}
	if row.ErrorKind != workflow.ErrorKindWindowClosed || len(row.ErrorChain) == 0 || !strings.Contains(row.ErrorChain[0], conversation.ErrWindowClosed.Error()) {
		t.Fatalf("kind = %q, chain = %q, want the technical text kept", row.ErrorKind, row.ErrorChain)
	}
}

func TestAFailureWithoutACodedMessageKeepsItsOwnText(t *testing.T) {
	row := stepRow("run-1", step{nodeID: "http", status: workflow.LogStatusFailed, err: errors.New("dial tcp: refused"), trace: &workflow.StepTrace{}})

	if row.Error != "dial tcp: refused" {
		t.Fatalf("error = %q", row.Error)
	}
}
