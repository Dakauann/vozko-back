package emailapi_repository

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/emailapi"
)

func TestListSQLBindsEveryPlaceholderAndNeverOffsetsOrCounts(t *testing.T) {
	at := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	cursor := emailapi.Cursor{CreatedAt: at, ID: "0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b"}
	for _, q := range []emailapi.LogQuery{
		{WorkspaceID: "ws", Limit: 50},
		{WorkspaceID: "ws", Status: emailapi.StatusDelivered, Limit: 50, Cursor: cursor},
		{WorkspaceID: "ws", Recipient: "a@b.com", From: at, To: at.Add(time.Hour), Limit: 10},
		{WorkspaceID: "ws", TagName: "kind", TagValue: "reset", Status: emailapi.StatusSent, Recipient: "a@b.com", From: at, To: at.Add(time.Hour), Cursor: cursor, Limit: 100},
	} {
		sql, args := ListSQL(q)
		if strings.Count(sql, "?") != len(args) {
			t.Fatalf("%d placeholders for %d args in %s", strings.Count(sql, "?"), len(args), sql)
		}
		upper := strings.ToUpper(sql)
		if strings.Contains(upper, "OFFSET") || strings.Contains(upper, "COUNT(") {
			t.Fatalf("the log must page by keyset only: %s", sql)
		}
		if args[len(args)-1] != q.Limit+1 {
			t.Fatalf("limit arg = %v, want one extra row to know there is a next page", args[len(args)-1])
		}
	}
}

func TestMarkStatusSQLBindsEveryPlaceholder(t *testing.T) {
	if strings.Count(markStatusSQL, "?") != 7 {
		t.Fatalf("placeholders = %d", strings.Count(markStatusSQL, "?"))
	}
}
