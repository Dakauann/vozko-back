package attendance_repository

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/attendance"
	"vozko/infra/database"
)

func TestOwnerResponseMeasuresTheCustomerAgainstTheOwnersOwnReply(t *testing.T) {
	sql := ownerResponseLateralsSQL()
	if !strings.Contains(sql, database.SentByContactSQL("m")) {
		t.Errorf("the customer's first message is not read from the sender:\n%s", sql)
	}
	if !strings.Contains(sql, "m.sender_id = "+ownerActorIDSQL) {
		t.Errorf("the reply is not the owner's own:\n%s", sql)
	}
	if strings.Contains(sql, "message_type") {
		t.Errorf("still infers the sender from the message type:\n%s", sql)
	}
}

func TestRespondedCountsOnlyTheOwnersOwnMessages(t *testing.T) {
	sql := respondedSQL("", "")
	if !strings.Contains(sql, "cm.sender_id = "+ownerActorIDSQL) || strings.Contains(sql, "message_type") {
		t.Fatalf("a colleague's message must not count as the owner responding:\n%s", sql)
	}
}

func TestFRTWaitsForAReplyToTheCustomer(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	sql, _ := frtSamplesQuery("ws-1", attendance.StatsFilter{DateFrom: &from}).build()
	if !strings.Contains(sql, database.SentAsReplySQL("m")) || strings.Contains(sql, "message_type") {
		t.Fatalf("FRT must wait for a reply sender:\n%s", sql)
	}
}
