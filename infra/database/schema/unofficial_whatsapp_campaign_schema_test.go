package schema

import (
	"reflect"
	"strings"
	"testing"
)

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
	return upper == len(name) && len(name) > 2
}

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
