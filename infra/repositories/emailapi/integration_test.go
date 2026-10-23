package emailapi_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailaddress"
	"vozko/domain/emailapi"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "email_api_test", append(schema.EmailAPIModels(), &schema.EmailMessageEvent{})...)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(40)
	return db
}

func draftSend(ws, key string, recipients int, at time.Time) (emailapi.Send, []emailapi.Message, []emailapi.Tag) {
	lease := at.Add(emailapi.LeaseDuration)
	send := emailapi.Send{
		ID: uuid.NewString(), WorkspaceID: ws, IdempotencyKey: key, Fingerprint: strings.Repeat("a", 64), Origin: emailapi.OriginAPI,
		SenderID: uuid.NewString(), From: "no-reply@acme.com", Subject: "Oi", Tags: map[string]string{"kind": "reset"},
		Recipients: recipients, LeaseUntil: &lease, CreatedAt: at,
	}
	messages := make([]emailapi.Message, recipients)
	for i := range messages {
		messages[i] = emailapi.Message{
			ID: uuid.NewString(), SendID: send.ID, WorkspaceID: ws, Position: i, Role: emailapi.RoleTo,
			Address: emailaddress.Address(fmt.Sprintf("p%d@example.com", i)), Status: emailapi.StatusPending, CreatedAt: at,
		}
	}
	return send, messages, []emailapi.Tag{{Name: "kind", Value: "reset"}}
}

func TestIntegrationConcurrentClaimsOfOneKeyCreateOneSend(t *testing.T) {
	db := integrationDB(t)
	sends := NewSends(db)
	ws := uuid.NewString()
	at := time.Now().UTC().Truncate(time.Microsecond)
	var created atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			send, messages, tags := draftSend(ws, "same-key", 3, at)
			res, err := sends.Claim(context.Background(), send, messages, tags)
			if err != nil {
				t.Error(err)
				return
			}
			if res.Created {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("created = %d, want exactly one send for one key", created.Load())
	}
	var rows int64
	db.Model(&schema.EmailAPIMessage{}).Count(&rows)
	if rows != 3 {
		t.Fatalf("message rows = %d", rows)
	}
}

func TestIntegrationOnlyOneCallTakesTheLease(t *testing.T) {
	db := integrationDB(t)
	sends := NewSends(db)
	ws := uuid.NewString()
	at := time.Now().UTC()
	send, messages, tags := draftSend(ws, "k", 1, at)
	send.LeaseUntil = nil
	if _, err := sends.Claim(context.Background(), send, messages, tags); err != nil {
		t.Fatal(err)
	}
	var taken atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := sends.TakeLease(context.Background(), ws, send.ID, at, at.Add(time.Minute))
			if err != nil {
				t.Error(err)
			}
			if ok {
				taken.Add(1)
			}
		}()
	}
	wg.Wait()
	if taken.Load() != 1 {
		t.Fatalf("leases taken = %d", taken.Load())
	}
	if ok, _ := sends.TakeLease(context.Background(), ws, send.ID, at.Add(2*time.Minute), at.Add(3*time.Minute)); !ok {
		t.Fatal("an expired lease must be taken again")
	}
	if err := sends.ReleaseLease(context.Background(), send.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := sends.TakeLease(context.Background(), uuid.NewString(), send.ID, at, at.Add(time.Minute)); ok {
		t.Fatal("another workspace took the lease")
	}
}

func TestIntegrationRoundsAndStatuses(t *testing.T) {
	db := integrationDB(t)
	sends := NewSends(db)
	logs := NewLog(db)
	ctx := context.Background()
	ws := uuid.NewString()
	at := time.Now().UTC().Truncate(time.Microsecond)
	send, messages, tags := draftSend(ws, "k", 3, at)
	if _, err := sends.Claim(ctx, send, messages, tags); err != nil {
		t.Fatal(err)
	}
	if err := sends.OpenRound(ctx, send.ID, 1, 3, 583, 165, []int{0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := sends.OpenRound(ctx, send.ID, 2, 3, 583, 165, []int{0}); !errors.Is(err, emailapi.ErrSendInProgress) {
		t.Fatalf("a second round over an open one = %v", err)
	}
	messageID := uuid.NewString()
	if err := sends.MarkSent(ctx, send.ID, 0, messageID, emailapi.StatusSent); err != nil {
		t.Fatal(err)
	}
	if err := sends.MarkRefused(ctx, send.ID, []int{0, 1, 2}, "email_send_throttled"); err != nil {
		t.Fatal(err)
	}
	got, _ := sends.Messages(ctx, send.ID)
	if got[0].Status != emailapi.StatusSent || got[0].MessageID != messageID || got[1].Status != emailapi.StatusRefused || got[2].ErrorCode != "email_send_throttled" || got[1].Round != 1 {
		t.Fatalf("messages = %+v", got)
	}
	stored, _ := sends.Get(ctx, send.ID)
	if !stored.RoundOpen() || stored.UnitsToRefund(got) != 2 {
		t.Fatalf("send = %+v", stored)
	}
	if err := sends.SettleRound(ctx, send.ID, 1); err != nil {
		t.Fatal(err)
	}
	if stored, _ := sends.Get(ctx, send.ID); stored.RoundOpen() {
		t.Fatal("the round is settled")
	}
	later := at.Add(time.Minute)
	if found, err := sends.MarkStatus(ctx, messageID, emailapi.StatusDelivered, "delivery", later); !found || err != nil {
		t.Fatalf("mark = %v %v", found, err)
	}
	if found, _ := sends.MarkStatus(ctx, messageID, emailapi.StatusDelivered, "send", at); !found {
		t.Fatal("an older event still updates the status")
	}
	entry, err := logs.GetByMessageID(ctx, messageID)
	if err != nil || entry.Status != emailapi.StatusDelivered || entry.LastEvent != "delivery" || entry.Send.Tags["kind"] != "reset" || entry.Send.IdempotencyKey != "k" {
		t.Fatalf("entry = %+v %v", entry, err)
	}
	if found, _ := sends.MarkStatus(ctx, uuid.NewString(), emailapi.StatusDelivered, "delivery", later); found {
		t.Fatal("an unknown message matched")
	}
	if _, err := logs.Get(ctx, uuid.NewString(), entry.ID); !errors.Is(err, emailapi.ErrMessageNotFound) {
		t.Fatalf("another workspace read the entry: %v", err)
	}
}

func seedLog(t *testing.T, db *gorm.DB, sends *Sends, ws string, n int, start time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		send, messages, tags := draftSend(ws, fmt.Sprintf("k-%d", i), 1, start.Add(time.Duration(i)*time.Second))
		messages[0].Address = emailaddress.Address(fmt.Sprintf("p%d@example.com", i%7))
		if i%3 == 0 {
			messages[0].Status = emailapi.StatusDelivered
		}
		if i%2 == 0 {
			tags = []emailapi.Tag{{Name: "kind", Value: "receipt"}}
			send.Tags = map[string]string{"kind": "receipt"}
		}
		if _, err := sends.Claim(context.Background(), send, messages, tags); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIntegrationLogPagesByKeysetUnderEveryFilter(t *testing.T) {
	db := integrationDB(t)
	sends := NewSends(db)
	logs := NewLog(db)
	ws := uuid.NewString()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	seedLog(t, db, sends, ws, 60, start)
	seedLog(t, db, sends, uuid.NewString(), 10, start)
	cases := []struct {
		name string
		q    emailapi.LogQuery
		want int
	}{
		{"all", emailapi.LogQuery{}, 60},
		{"status", emailapi.LogQuery{Status: emailapi.StatusDelivered}, 20},
		{"recipient", emailapi.LogQuery{Recipient: "p3@example.com"}, 9},
		{"tag", emailapi.LogQuery{TagName: "kind", TagValue: "receipt"}, 30},
		{"tag and status", emailapi.LogQuery{TagName: "kind", TagValue: "receipt", Status: emailapi.StatusDelivered}, 10},
		{"date", emailapi.LogQuery{From: start.Add(10 * time.Second), To: start.Add(20 * time.Second)}, 10},
	}
	for _, c := range cases {
		q := c.q
		q.WorkspaceID = ws
		q.Limit = 7
		seen := map[string]bool{}
		var previous time.Time
		for pages := 0; ; pages++ {
			if pages > 20 {
				t.Fatalf("%s: never ended", c.name)
			}
			rows, err := logs.List(context.Background(), q)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			page := emailapi.PageOf(rows, q.Limit)
			for _, e := range page.Items {
				if seen[e.ID] || e.WorkspaceID != ws {
					t.Fatalf("%s: repeated or foreign row %s", c.name, e.ID)
				}
				if !previous.IsZero() && e.CreatedAt.After(previous) {
					t.Fatalf("%s: out of order", c.name)
				}
				previous = e.CreatedAt
				seen[e.ID] = true
			}
			if page.Next == "" {
				break
			}
			cursor, err := emailapi.ParseCursor(page.Next)
			if err != nil {
				t.Fatal(err)
			}
			q.Cursor = cursor
		}
		if len(seen) != c.want {
			t.Fatalf("%s: rows = %d, want %d", c.name, len(seen), c.want)
		}
	}
}

func TestIntegrationLogQueriesUseTheirIndexWithoutSortingOrScanning(t *testing.T) {
	db := integrationDB(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ws := uuid.NewString()
	for _, workspace := range []string{ws, uuid.NewString(), uuid.NewString(), uuid.NewString()} {
		bulkLog(t, db, workspace, 50_000, start)
	}
	for _, table := range []string{"email_api_messages", "email_api_message_tags", "email_api_sends"} {
		if err := db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	cursor := emailapi.Cursor{CreatedAt: start.Add(30 * time.Minute), ID: uuid.NewString()}
	cases := []struct {
		q     emailapi.LogQuery
		index string
	}{
		{emailapi.LogQuery{WorkspaceID: ws, Limit: 50, Cursor: cursor}, "idx_email_api_messages_log"},
		{emailapi.LogQuery{WorkspaceID: ws, Status: emailapi.StatusDelivered, Limit: 50, Cursor: cursor}, "idx_email_api_messages_status"},
		{emailapi.LogQuery{WorkspaceID: ws, Recipient: "p3@example.com", Limit: 50, Cursor: cursor}, "idx_email_api_messages_address"},
		{emailapi.LogQuery{WorkspaceID: ws, TagName: "kind", TagValue: "receipt", Limit: 50, Cursor: cursor}, "idx_email_api_message_tags_filter"},
		{emailapi.LogQuery{WorkspaceID: ws, From: start, To: start.Add(time.Minute), Limit: 50}, "idx_email_api_messages_log"},
	}
	for _, c := range cases {
		sql, args := ListSQL(c.q)
		var plan []string
		if err := db.Raw("EXPLAIN "+sql, args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		text := strings.Join(plan, "\n")
		if !strings.Contains(text, c.index) {
			t.Fatalf("plan does not use %s:\n%s", c.index, text)
		}
		if strings.Contains(text, "Seq Scan on email_api_messages") || strings.Contains(text, "Seq Scan on email_api_message_tags") || strings.Contains(text, "Sort ") {
			t.Fatalf("plan scans or sorts:\n%s", text)
		}
		t.Logf("%s:\n%s", c.index, text)
	}
}

func bulkLog(t *testing.T, db *gorm.DB, ws string, n int, start time.Time) {
	t.Helper()
	sendID := uuid.NewString()
	steps := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO email_api_sends (id, workspace_id, idempotency_key, fingerprint, origin, sender_id, from_address, subject, tags, recipients, round_settled, created_at, updated_at)
			VALUES (?, ?, 'bulk', repeat('a', 64), 'api', gen_random_uuid(), 'no-reply@acme.com', 'Oi', '{}', ?, true, ?, ?)`, []any{sendID, ws, n, start, start}},
		{`INSERT INTO email_api_messages (id, send_id, position, workspace_id, role, address, status, error_code, round, last_event, created_at, updated_at)
			SELECT gen_random_uuid(), ?, g, ?, 'to', 'p' || (g % 500) || '@example.com',
				CASE WHEN g % 10 = 0 THEN 'bounced' WHEN g % 3 = 0 THEN 'sent' ELSE 'delivered' END, '', 1, '',
				?::timestamptz + g * interval '50 milliseconds', ?::timestamptz
			FROM generate_series(0, ?) g`, []any{sendID, ws, start, start, n - 1}},
		{`INSERT INTO email_api_message_tags (message_row_id, name, workspace_id, value, created_at)
			SELECT id, 'kind', workspace_id, CASE WHEN position % 4 = 0 THEN 'receipt' ELSE 'reset' END, created_at
			FROM email_api_messages WHERE send_id = ?`, []any{sendID}},
	}
	for _, s := range steps {
		if err := db.Exec(s.sql, s.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
}
