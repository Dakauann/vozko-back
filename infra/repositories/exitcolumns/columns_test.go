package exitcolumns

import (
	"testing"
	"time"

	"vozko/domain/buttonaction"
)

func TestAnAcknowledgementRoundTripsThroughItsColumnsInUTC(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	by, stored := AcknowledgementColumns(&buttonaction.Acknowledgement{By: "user-1", At: at})
	if by == nil || stored == nil || *by != "user-1" || stored.Location() != time.UTC {
		t.Fatalf("columns = %v, %v", by, stored)
	}
	back := AcknowledgementOf(by, stored)
	if back == nil || back.By != "user-1" || !back.At.Equal(at) {
		t.Fatalf("back = %+v", back)
	}
}

func TestNoAcknowledgementIsNoColumnsAndBack(t *testing.T) {
	if by, at := AcknowledgementColumns(nil); by != nil || at != nil {
		t.Fatalf("columns = %v, %v", by, at)
	}
	user := "user-1"
	if AcknowledgementOf(&user, nil) != nil || AcknowledgementOf(nil, nil) != nil {
		t.Fatal("a half stored acknowledgement is none")
	}
}

func TestOnlyAUUIDIsAValidID(t *testing.T) {
	if !ValidID("6f1d1c52-2a6c-4b8e-9f43-1c1f5b7d2a01") || ValidID("ws-1") || ValidID("") {
		t.Fatal("ValidID")
	}
}
