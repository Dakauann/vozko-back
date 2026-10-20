package pipeline_repository

import "testing"

func TestEveryChannelAccountThatPicksAFunnelCountsAsItsUse(t *testing.T) {
	bound := map[string]string{}
	for _, b := range pipelineBindings {
		bound[b.table] = b.kind
	}
	for _, table := range []string{"instagram_accounts", "telegram_accounts", "webchat_widgets", "unofficial_whatsapp_instances", "mercadolivre_accounts"} {
		if bound[table] != "channel" {
			t.Errorf("%s picks a funnel but deleting the funnel would not see it", table)
		}
	}
}
