package whatsapp_campaign_entry

import (
	"strings"
	"testing"

	"vozko/infra/database"
)

func TestARepliedEntryIsOneTheContactWroteBackTo(t *testing.T) {
	for name, sql := range map[string]string{"funnel": repliedSQL, "daily": dailySQL} {
		if !strings.Contains(sql, database.SentByContactSQL("m")) {
			t.Errorf("%s does not read who sent the message:\n%s", name, sql)
		}
		if strings.Contains(sql, "@inbound") {
			t.Errorf("%s still infers the customer from the message type", name)
		}
	}
}
