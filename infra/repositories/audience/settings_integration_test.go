package audience_repository

import (
	"context"
	"reflect"
	"testing"

	ca "vozko/domain/audience"
	"vozko/infra/database/schema"
)

// A column added to the settings table but forgotten in the upsert's DoUpdates
// list saves on a NEW row and silently does nothing on an existing one. The API
// returns what you sent, the next read returns the old value, and the operator
// concludes the toggle is broken.
//
// It has happened twice: once on the author projection's reputation column, and
// once on reply_mode, which is what made "Resposta aos comentários" impossible
// to switch on. These tests make the third time a test failure.

func TestIntegration_SettingsUpdateSurvivesAnExistingRow(t *testing.T) {
	db := integrationDB(t)
	repo := NewSettingsRepository(db)
	ctx := context.Background()
	ws := "11111111-1111-1111-1111-111111111111"
	account := integrationRef().AccountID

	first := ca.NewSettings(ws, ca.SourceInstagram, account, ca.VerticalGov)
	first.Enabled = true
	first.Normalize()
	if err := repo.Save(ctx, &first); err != nil {
		t.Fatal(err)
	}

	// Now change EVERY operator-settable field on the existing row.
	next, err := repo.Find(ctx, ca.SourceInstagram, account)
	if err != nil {
		t.Fatal(err)
	}
	next.Enabled = false
	next.Model = "model-y"
	next.Vertical = ca.VerticalRetail
	next.ActionPolicy = ca.ActionPolicy{SeverityThreshold: 42}
	next.DailyCap = 1234
	next.Instructions = "somos uma loja"
	next.ReplyPolicy = ca.ReplyPolicy{Mode: ca.ReplyModeSuggest, MaxAutoSeverity: 25}
	next.Normalize()
	if err := repo.Save(ctx, next); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.Find(ctx, ca.SourceInstagram, account)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Enabled {
		t.Error("enabled did not persist")
	}
	if stored.Model != "model-y" {
		t.Errorf("model = %q", stored.Model)
	}
	if stored.Vertical != ca.VerticalRetail {
		t.Errorf("vertical = %q", stored.Vertical)
	}
	if stored.ActionPolicy.SeverityThreshold != 42 {
		t.Errorf("severity threshold = %d", stored.ActionPolicy.SeverityThreshold)
	}
	if stored.DailyCap != 1234 {
		t.Errorf("daily cap = %d", stored.DailyCap)
	}
	if stored.Instructions != "somos uma loja" {
		t.Errorf("instructions = %q", stored.Instructions)
	}
	// THE one that was broken: switching replying on read back as off.
	if stored.ReplyPolicy.Mode != ca.ReplyModeSuggest {
		t.Errorf("reply mode = %q, want suggest: the setting cannot be switched on", stored.ReplyPolicy.Mode)
	}
	if stored.ReplyPolicy.MaxAutoSeverity != 25 {
		t.Errorf("reply ceiling = %d", stored.ReplyPolicy.MaxAutoSeverity)
	}
}

// The alert rules table has the same trap, and its Update writes an explicit
// column map. A field added to the rule but forgotten there is a setting the
// operator can choose once and never change.
func TestIntegration_AlertRuleUpdateWritesEveryEditableField(t *testing.T) {
	db := integrationDB(t)
	repo := NewAlertRuleRepository(db)
	ctx := context.Background()
	ws := "11111111-1111-1111-1111-111111111111"
	rule := seedAlertRule(t, repo, ws, nil)

	edited, err := repo.FindByID(ctx, ws, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited.Name = "Outro nome"
	edited.Enabled = false
	edited.Metric = ca.AlertMetricHostileCount
	edited.Threshold = 7
	edited.WindowMinutes = 120
	edited.Channel = ca.AlertChannelOfficial
	edited.Recipient = "5511888888888"
	edited.BusinessPhoneID = "55555555-5555-5555-5555-555555555555"
	edited.TemplateID = "66666666-6666-6666-6666-666666666666"
	edited.Brief = true
	edited.CooldownMinutes = 90
	edited.MaxPerDay = 4
	edited.Normalize()
	if err := repo.Update(ctx, edited); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.FindByID(ctx, ws, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		field string
		got   any
		want  any
	}{
		{"name", stored.Name, "Outro nome"},
		{"enabled", stored.Enabled, false},
		{"metric", stored.Metric, ca.AlertMetricHostileCount},
		{"threshold", stored.Threshold, 7},
		{"window", stored.WindowMinutes, 120},
		{"channel", stored.Channel, ca.AlertChannelOfficial},
		{"recipient", stored.Recipient, "5511888888888"},
		{"business phone", stored.BusinessPhoneID, "55555555-5555-5555-5555-555555555555"},
		{"template", stored.TemplateID, "66666666-6666-6666-6666-666666666666"},
		{"brief", stored.Brief, true},
		{"cooldown", stored.CooldownMinutes, 90},
		{"max per day", stored.MaxPerDay, 4},
	} {
		if c.got != c.want {
			t.Errorf("%s did not persist: got %v, want %v", c.field, c.got, c.want)
		}
	}
}

// The structural version of the same check: every column the settings table
// has, apart from the key and the bookkeeping, must appear in the upsert's
// update list. This fails the moment a column is added without one.
func TestIntegration_SettingsUpsertUpdatesEveryColumn(t *testing.T) {
	db := integrationDB(t)

	// The primary key identifies the row and must not be reassigned;
	// created_at belongs to the first write.
	skip := map[string]bool{"source": true, "account_id": true, "created_at": true}

	stmt := db.Model(&schema.AudienceSettings{}).Statement
	if err := stmt.Parse(&schema.AudienceSettings{}); err != nil {
		t.Fatal(err)
	}

	var columns []string
	for _, field := range stmt.Schema.Fields {
		name := field.DBName
		if name == "" || skip[name] {
			continue
		}
		columns = append(columns, name)
	}

	updated := settingsUpsertColumns()
	inUpdate := make(map[string]bool, len(updated))
	for _, c := range updated {
		inUpdate[c] = true
	}

	var missing []string
	for _, c := range columns {
		if !inUpdate[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("columns absent from the settings upsert, so they save on a new row and are ignored on an existing one: %v", missing)
	}

	// And nothing in the list that is not a real column, which would make the
	// write fail outright.
	known := make(map[string]bool, len(columns))
	for _, c := range columns {
		known[c] = true
	}
	for _, c := range updated {
		if !known[c] && !skip[c] {
			t.Fatalf("the settings upsert names %q, which is not a column", c)
		}
	}
	if reflect.DeepEqual(columns, updated) {
		// Order does not matter; this is only here so the slices are used.
		_ = columns
	}
}
