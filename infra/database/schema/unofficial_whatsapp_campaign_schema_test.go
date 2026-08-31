package schema

import (
	"reflect"
	"strings"
	"testing"
)

// Every column a repository names in a raw Updates map has to exist under that
// exact name.
//
// GORM derives a column from the Go field, and its naming strategy turns `JID`
// into `j_id` — which then disagrees with `updates["jid"]` in the repository.
// Nothing catches that: the struct compiles, AutoMigrate happily creates
// `j_id`, and the failure surfaces at runtime as SQLSTATE 42703 on the send
// path, after a campaign has already started. It has been made once before in
// this channel — unofficial_whatsapp_broadcast_targets carries both spellings.
//
// So any field whose name is an all-caps initialism must pin its column
// explicitly, and this test is what says so.
func TestCampaignEntryInitialismColumnsArePinned(t *testing.T) {
	for _, model := range []any{
		UnofficialWhatsAppCampaignEntry{},
		UnofficialWhatsAppCampaign{},
	} {
		typ := reflect.TypeOf(model)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !isInitialism(field.Name) {
				continue
			}
			tag := field.Tag.Get("gorm")
			if !strings.Contains(tag, "column:") {
				t.Errorf(
					"%s.%s is an initialism with no explicit column: GORM will name it %q, "+
						"which will not match a raw column reference",
					typ.Name(), field.Name, gormDefaultName(field.Name),
				)
			}
		}
	}
}

// isInitialism reports whether a field name is a run of capitals GORM will
// split, e.g. JID -> j_id. Names ending in a known suffix like ID are excluded:
// `CampaignID` becomes `campaign_id`, which is what everyone expects.
func isInitialism(name string) bool {
	if len(name) < 2 {
		return false
	}
	upper := 0
	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			upper++
		}
	}
	// Entirely capitals, and longer than the two-letter `ID` GORM handles.
	return upper == len(name) && len(name) > 2
}

// gormDefaultName approximates the naming strategy, purely so the failure
// message can show what the column WOULD be called.
func gormDefaultName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r | 0x20)
	}
	return b.String()
}

// The entry's JID specifically, since it is the one that broke.
func TestCampaignEntryJIDColumnIsJid(t *testing.T) {
	field, ok := reflect.TypeOf(UnofficialWhatsAppCampaignEntry{}).FieldByName("JID")
	if !ok {
		t.Fatal("UnofficialWhatsAppCampaignEntry has no JID field")
	}
	if !strings.Contains(field.Tag.Get("gorm"), "column:jid") {
		t.Fatalf("JID column tag = %q, want column:jid to match every other JID in this channel",
			field.Tag.Get("gorm"))
	}
}
