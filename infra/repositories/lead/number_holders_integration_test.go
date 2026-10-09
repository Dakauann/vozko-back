package lead

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/infra/database"
)

func holdersDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := dialDirectoryDB(t)
	identity, ok := database.PerformanceIndexSQL("ux_leads_workspace_number")
	if !ok {
		t.Fatal("ux_leads_workspace_number is not declared")
	}
	phones, ok := database.ConcurrentIndexSQL(database.LeadPhoneHoldersIndex)
	if !ok {
		t.Fatalf("%s is not declared", database.LeadPhoneHoldersIndex)
	}
	for _, ddl := range []string{identity, phones} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("index: %v", err)
		}
	}
	return db
}

func seedHolders(t *testing.T, db *gorm.DB, rows ...[]any) {
	t.Helper()
	for _, row := range rows {
		if err := db.Exec(row[0].(string), row[1:]...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func TestIntegrationOtherHoldersFindsLiveHoldersOfBothFormsButNeverTheLeadItself(t *testing.T) {
	db := holdersDB(t)
	ws, otherWS := uuid.NewString(), uuid.NewString()
	self, sister, twin, deleted, stranger := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	seed := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '5584994409684', 'Maria')`, []any{self, ws}},
		{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '558433334444', 'landline')`, []any{uuid.NewString(), ws, self}},
		{`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '5584988887777', 'Irmã')`, []any{sister, ws}},
		{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '558433334444', 'landline')`, []any{uuid.NewString(), ws, sister}},
		{`INSERT INTO leads (id, workspace_id, name) VALUES (?, ?, 'Gêmeo')`, []any{twin, ws}},
		{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '558494409684', 'mobile')`, []any{uuid.NewString(), ws, twin}},
		{`INSERT INTO leads (id, workspace_id, name, deleted_at) VALUES (?, ?, 'Apagada', now())`, []any{deleted, ws}},
		{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '558433334444', 'landline')`, []any{uuid.NewString(), ws, deleted}},
		{`INSERT INTO leads (id, workspace_id, name) VALUES (?, ?, 'Outro workspace')`, []any{stranger, otherWS}},
		{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '558433334444', 'landline')`, []any{uuid.NewString(), otherWS, stranger}},
	}
	for _, row := range seed {
		if err := db.Exec(row.sql, row.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got, err := NewNumberDirectory(db).OtherHolders(context.Background(), ws, self, []string{"5584994409684", "558433334444"})
	if err != nil {
		t.Fatalf("OtherHolders: %v", err)
	}
	ids := map[string]bool{}
	for _, l := range got {
		ids[l.ID] = true
	}
	if len(got) != 2 || !ids[sister] || !ids[twin] {
		t.Fatalf("OtherHolders = %v, want the sister and the ninth digit twin only", ids)
	}
	for _, l := range got {
		if l.Phones == nil {
			t.Fatalf("holder %s came without its phones", l.ID)
		}
	}
}

func TestIntegrationOtherHoldersReadsMoreThanTheLimitOfAWidelySharedNumber(t *testing.T) {
	db := holdersDB(t)
	ws, self := uuid.NewString(), uuid.NewString()
	if err := db.Exec(`INSERT INTO leads (id, workspace_id, name) VALUES (?, ?, 'Escola')`, self, ws).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < lead.SharedNumberHolderLimit*3; i++ {
		holder := uuid.NewString()
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, name) VALUES (?, ?, 'Responsável')`, holder, ws).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := db.Exec(`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '551133334444', 'landline')`, uuid.NewString(), ws, holder).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got, err := NewNumberDirectory(db).OtherHolders(context.Background(), ws, self, []string{"551133334444"})
	if err != nil {
		t.Fatalf("OtherHolders: %v", err)
	}
	if len(got) <= lead.SharedNumberHolderLimit || len(got) > 2*(lead.SharedNumberHolderLimit+1) {
		t.Fatalf("holders read = %d, want more than the limit and a bounded read", len(got))
	}
	shared := lead.SharedNumbersOf(&lead.Lead{ID: self, Phones: []lead.ContactPhone{{Number: "551133334444"}}}, got)
	if len(shared) != 1 || !shared[0].More {
		t.Fatalf("shared = %+v, want the number marked as held by more", shared)
	}
}

func TestIntegrationOtherHoldersFindsTheLeadWhoseWhatsAppIsTheOtherFormOfAContactPhone(t *testing.T) {
	db := holdersDB(t)
	ws, self, twin := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedHolders(t, db,
		[]any{`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '5584988887777', 'Maria')`, self, ws},
		[]any{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '5511987654321', 'mobile')`, uuid.NewString(), ws, self},
		[]any{`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '551187654321', 'Filho')`, twin, ws},
	)

	loaded, err := NewNumberDirectory(db).LoadForDial(context.Background(), ws, self)
	if err != nil {
		t.Fatalf("LoadForDial: %v", err)
	}
	got, err := NewNumberDirectory(db).OtherHolders(context.Background(), ws, self, loaded.Numbers())
	if err != nil {
		t.Fatalf("OtherHolders: %v", err)
	}
	shared := lead.SharedNumbersOf(loaded, got)
	if len(shared) != 1 || shared[0].Number != "5511987654321" || len(shared[0].Holders) != 1 || shared[0].Holders[0].LeadID != twin {
		t.Fatalf("shared = %+v, want the contact phone held by the lead whose WhatsApp is its other form", shared)
	}
}

func TestIntegrationOtherHoldersReadsTheSameSampleOfAWidelySharedNumberEveryTime(t *testing.T) {
	db := holdersDB(t)
	ws, self := uuid.NewString(), uuid.NewString()
	seedHolders(t, db, []any{`INSERT INTO leads (id, workspace_id, name) VALUES (?, ?, 'Escola')`, self, ws})
	holders := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		holder := uuid.NewString()
		holders = append(holders, holder)
		seedHolders(t, db,
			[]any{`INSERT INTO leads (id, workspace_id, name) VALUES (?, ?, 'Responsável')`, holder, ws},
			[]any{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '551133334444', 'landline')`, uuid.NewString(), ws, holder},
		)
	}
	sort.Strings(holders)

	var reads [][]string
	for i := 0; i < 3; i++ {
		got, err := NewNumberDirectory(db).OtherHolders(context.Background(), ws, self, []string{"551133334444"})
		if err != nil {
			t.Fatalf("OtherHolders: %v", err)
		}
		ids := make([]string, 0, len(got))
		for _, l := range got {
			ids = append(ids, l.ID)
		}
		sort.Strings(ids)
		reads = append(reads, ids)
	}
	want := holders[:lead.SharedNumberHolderLimit+1]
	for i, ids := range reads {
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("read %d = %v, want the first %d holders by lead id", i, ids, len(want))
		}
	}
}

func TestIntegrationOtherHoldersReadsBothBucketsThroughTheirIndexes(t *testing.T) {
	db := holdersDB(t)
	ws, self := uuid.NewString(), uuid.NewString()
	seedHolders(t, db,
		[]any{`INSERT INTO leads (id, workspace_id, number, name) SELECT gen_random_uuid(), ?::uuid, (5511900000000 + g)::text, 'Lead ' || g FROM generate_series(1, 20000) g`, ws},
		[]any{`INSERT INTO leads (id, workspace_id, number, name) SELECT gen_random_uuid(), gen_random_uuid(), (5511900000000 + g)::text, 'Outro ' || g FROM generate_series(1, 20000) g`},
		[]any{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) SELECT gen_random_uuid(), l.workspace_id, l.id, (551130000000 + row_number() OVER ())::text, 'landline' FROM leads l`},
	)
	seedHolders(t, db, []any{`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '5511987654321', 'Maria')`, self, ws})
	if err := db.Exec("ANALYZE leads; ANALYZE lead_phones").Error; err != nil {
		t.Fatalf("analyze: %v", err)
	}

	var plan string
	formats := numberFormats([]string{"5511987654321", "551130000042"})
	perForm := lead.SharedNumberHolderLimit + 1
	if err := db.Raw("EXPLAIN (FORMAT JSON) "+otherHoldersSQL, ws, formats, ws, self, perForm, ws, self, perForm).Row().Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal([]byte(plan), &nodes); err != nil || len(nodes) == 0 {
		t.Fatalf("plan = %s, %v", plan, err)
	}
	for _, scan := range seqScans(nodes[0]["Plan"]) {
		if scan == "leads" || scan == "lead_phones" {
			t.Fatalf("the holders read scans the whole %s table: %s", scan, plan)
		}
	}
}

func seqScans(node any) []string {
	plan, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	var found []string
	if plan["Node Type"] == "Seq Scan" {
		relation, _ := plan["Relation Name"].(string)
		found = append(found, relation)
	}
	children, _ := plan["Plans"].([]any)
	for _, child := range children {
		found = append(found, seqScans(child)...)
	}
	return found
}
