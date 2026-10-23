package emailsending_repository

import (
	"errors"
	"testing"

	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
)

func TestDomainRowsWithAnUnknownConnectMethodAreRefused(t *testing.T) {
	for _, method := range []string{"entri", ""} {
		if _, err := toDomain(schema.EmailSendingDomain{ID: "d1", ConnectMethod: method}); !errors.Is(err, es.ErrUnknownConnectMethod) {
			t.Errorf("connect method %q: err = %v", method, err)
		}
	}
	for _, method := range []es.ConnectMethod{es.ConnectAutomatic, es.ConnectManual} {
		if _, err := toDomain(schema.EmailSendingDomain{ID: "d1", ConnectMethod: string(method)}); err != nil {
			t.Errorf("connect method %q: err = %v", method, err)
		}
	}
}

func TestDomainRowsKeepTheirSetupFailure(t *testing.T) {
	d := es.SendingDomain{ID: "d1", ConnectMethod: es.ConnectAutomatic, Status: es.DomainPending, SetupFailure: es.SetupFailureReplyCapacity}
	row, err := toDomainSchema(&d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := toDomain(*row)
	if err != nil || back.SetupFailure != es.SetupFailureReplyCapacity {
		t.Fatalf("round trip = %+v %v", back, err)
	}
	if _, err := toDomain(schema.EmailSendingDomain{ID: "d1", ConnectMethod: string(es.ConnectAutomatic), SetupFailure: "timeout"}); !errors.Is(err, es.ErrUnknownSetupFailure) {
		t.Fatalf("an unknown setup failure must be refused: %v", err)
	}
}
