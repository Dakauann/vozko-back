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
