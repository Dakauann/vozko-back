package marketing

import (
	"context"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func TestListActivitiesReadsTheObjectAndItsChildrenInTheViewersLanguage(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[
		{"event_type":"update_campaign_run_status","translated_event_type":"Status do conjunto de anúncios atualizado","event_time":"2026-10-03T23:33:10+0000","actor_name":"Dakauann","object_id":"120201","object_name":"Conjunto SP","object_type":"AD_SET","extra_data":"{\"old_value\":\"Ativo\",\"new_value\":\"Inativo\"}"},
		{"event_type":"update_ad_set_budget","translated_event_type":"Orçamento atualizado","event_time":"2026-10-03T23:30:00+0000","actor_name":"Meta","object_id":"120201","object_name":"Conjunto SP","object_type":"AD_SET","extra_data":"{\"old_value\":{\"amount\":1000},\"new_value\":{\"amount\":1200}}"}
	]}`))
	since := time.Date(2026, 9, 5, 3, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)
	got, err := g.ListActivities(context.Background(), "tok", "111", advertising.ActivityQuery{ObjectID: "120201", Since: since, Until: until, Locale: "pt_BR"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Label != "Status do conjunto de anúncios atualizado" || got[0].From != "Ativo" || got[0].To != "Inativo" || got[0].ActorName != "Dakauann" {
		t.Fatalf("got %+v", got)
	}
	if got[1].From != "" || got[1].To != "" {
		t.Fatalf("a structured change was flattened: %+v", got[1])
	}
	q := (*calls)[0].query
	if (*calls)[0].path != "/v26.0/act_111/activities" || q.Get("oid") != "120201" || q.Get("add_children") != "true" || q.Get("locale") != "pt_BR" || q.Get("since") != "1788577200" {
		t.Fatalf("call %+v", (*calls)[0])
	}
}
