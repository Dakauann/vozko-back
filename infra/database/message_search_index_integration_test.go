package database

import (
	"strings"
	"testing"

	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
	"vozko/infra/repositories/repotest"
)

func TestMessageTextSearchUsesTheTrigramIndexAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "msg_search", &schema.ConversationMessage{})
	if err := createIndexConcurrently(db, declaredConcurrentIndex(t, MessageTextSearchIndex)); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if !indexExists(t, db, MessageTextSearchIndex) {
		t.Fatal("the trigram index was not built")
	}
	if err := createIndexConcurrently(db, declaredConcurrentIndex(t, MessageTextSearchIndex)); err != nil {
		t.Fatalf("a second boot must be a no op, got %v", err)
	}

	for _, sql := range []string{
		`INSERT INTO conversation_messages (id, entry_id, entry_type, text, transcript, created_at, updated_at)
			SELECT md5(i::text), uuid_in(md5((i % 2000)::text)::cstring), 'whatsapp',
			       'mensagem ' || md5(i::text), CASE WHEN i % 7 = 0 THEN 'audio ' || md5((i * 3)::text) ELSE '' END,
			       NOW() - (i || ' minutes')::interval, NOW()
			FROM generate_series(1, 60000) AS i`,
		`UPDATE conversation_messages SET transcript = 'quero o plano anual' WHERE id = md5('42')`,
		`ANALYZE conversation_messages`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	inboxSearch := `SELECT DISTINCT cm2.entry_id FROM conversation_messages cm2
		WHERE cm2.entry_type = ? AND ` + sqlexpr.MessageTextMatches("cm2")
	var plan []string
	if err := db.Raw("EXPLAIN "+inboxSearch, "whatsapp", "%plano anual%").Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("inbox message search plan:\n%s", strings.Join(plan, "\n"))
	if !strings.Contains(strings.Join(plan, "\n"), MessageTextSearchIndex) {
		t.Fatalf("the inbox message search must use %s, plan:\n%s", MessageTextSearchIndex, strings.Join(plan, "\n"))
	}

	var found []string
	if err := db.Raw(inboxSearch, "whatsapp", "%PLANO ANUAL%").Scan(&found).Error; err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d entries, want the one whose transcript matches", len(found))
	}
}
