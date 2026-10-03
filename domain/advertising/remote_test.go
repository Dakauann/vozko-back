package advertising

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassifyReadsTheRemoteErrorThroughWrapping(t *testing.T) {
	err := fmt.Errorf("create campaign: %w", &RemoteError{Kind: FailureRejected, Code: 100, UserMessage: "Orçamento abaixo do mínimo"})
	if Classify(err) != FailureRejected {
		t.Fatalf("kind %s", Classify(err))
	}
	if Explain(err) != "Orçamento abaixo do mínimo" || FailureCode(err) != "meta_100" {
		t.Fatalf("explain %q code %q", Explain(err), FailureCode(err))
	}
}

func TestErrorsThatAreNotFromMetaAreUnknownNotRejected(t *testing.T) {
	if Classify(errors.New("connection reset")) != FailureUnknown {
		t.Fatal("transport error treated as a definite answer")
	}
	if Classify(&RemoteError{}) != FailureUnknown {
		t.Fatal("blank kind treated as definite")
	}
}

func TestKnownMetaSubcodesHaveAReason(t *testing.T) {
	budget := fmt.Errorf("create ad set: %w", &RemoteError{Kind: FailureRejected, Code: 100, Subcode: 1885272})
	if ReasonOf(budget) != ReasonBudgetTooLow {
		t.Fatalf("budget reason %q", ReasonOf(budget))
	}
	readOnly := &RemoteError{Kind: FailurePermission, Code: 200, Subcode: 2490585}
	if ReasonOf(readOnly) != ReasonAccountReadOnly {
		t.Fatalf("read only reason %q", ReasonOf(readOnly))
	}
	if ReasonOf(&RemoteError{Code: 100, Subcode: 1}) != ReasonNone || ReasonOf(errors.New("x")) != ReasonNone {
		t.Fatal("unrelated error has a reason")
	}
}

func TestMissingPaymentMethodAtMetaHasAReason(t *testing.T) {
	err := fmt.Errorf("create ad: %w", &RemoteError{Kind: FailureRejected, Code: 100, Subcode: 1359188})
	if ReasonOf(err) != ReasonNoPaymentMethod {
		t.Fatalf("reason %q", ReasonOf(err))
	}
}
