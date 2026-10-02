package marketing

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/advertising"
)

func TestListAdAccountsFollowsPagingAndMapsFunding(t *testing.T) {
	g, calls := gatewayWith(t, func(call recordedCall) (int, string) {
		if call.query.Get("after") == "" {
			return http.StatusOK, `{"data":[{"account_id":"111","name":"Loja","currency":"BRL","timezone_name":"America/Sao_Paulo",
				"account_status":1,"disable_reason":0,"funding_source":"9001","amount_spent":"123456","spend_cap":"",
				"business":{"id":"B1","name":"Loja SA"},"user_tasks":["ANALYZE","ADVERTISE"]}],"paging":{"cursors":{"after":"C1"},"next":"https://graph.facebook.com/next"}}`
		}
		return http.StatusOK, `{"data":[{"account_id":"222","name":"Outra","currency":"USD","account_status":2,"disable_reason":3,
			"amount_spent":"0","spend_cap":"50000","user_tasks":["ANALYZE"]}],"paging":{"cursors":{"after":"C2"}}}`
	})

	accounts, err := g.ListAdAccounts(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[1].query.Get("after") != "C1" {
		t.Fatalf("calls = %+v", *calls)
	}
	first := (*calls)[0]
	if first.path != "/v26.0/me/adaccounts" || first.query.Get("limit") != "100" || !strings.Contains(first.query.Get("fields"), "funding_source") || !strings.Contains(first.query.Get("fields"), "business{id,name}") || !strings.Contains(first.query.Get("fields"), "user_tasks") || first.query.Get("appsecret_proof") == "" {
		t.Fatalf("first call = %+v", first)
	}
	want := []advertising.RemoteAdAccount{
		{MetaAccountID: "111", Name: "Loja", BusinessID: "B1", BusinessName: "Loja SA", Currency: "BRL", Timezone: "America/Sao_Paulo", Status: advertising.MetaAccountActive, HasFunding: true, AmountSpent: 123456, Tasks: []string{"ANALYZE", "ADVERTISE"}},
		{MetaAccountID: "222", Name: "Outra", Currency: "USD", Status: advertising.MetaAccountDisabled, DisableReason: 3, SpendCap: 50000, Tasks: []string{"ANALYZE"}},
	}
	if len(accounts) != len(want) {
		t.Fatalf("accounts = %+v", accounts)
	}
	for i := range want {
		if !reflect.DeepEqual(accounts[i], want[i]) {
			t.Fatalf("account %d = %+v, want %+v", i, accounts[i], want[i])
		}
	}
}

func TestGetAdAccountStripsActPrefixAndRejectsBadAmounts(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"account_id":"111","currency":"BRL","account_status":1,"amount_spent":"12.5"}`))

	_, err := g.GetAdAccount(context.Background(), "tok", "act_111")
	if err == nil {
		t.Fatal("expected an error for a non integer amount")
	}
	if (*calls)[0].path != "/v26.0/act_111" {
		t.Fatalf("path = %s", (*calls)[0].path)
	}
}

func TestListPagesMapsAdvertisingTasksAndInstagram(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[
		{"id":"P1","name":"Loja","tasks":["advertise","ANALYZE"],"picture":{"data":{"url":"https://pic"}},"whatsapp_number":"+55 11 99999-0000","leadgen_tos_accepted":true,
		 "instagram_business_account":{"id":"IG1","username":"loja"}},
		{"id":"P2","name":"Blog","tasks":["ANALYZE","CREATE_CONTENT"]},
		{"id":"P3","name":"Dono","tasks":["MANAGE"]}]}`))

	pages, err := g.ListPages(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/v26.0/me/accounts" || !strings.Contains((*calls)[0].query.Get("fields"), "whatsapp_number") ||
		!strings.Contains((*calls)[0].query.Get("fields"), "leadgen_tos_accepted") {
		t.Fatalf("call = %+v", (*calls)[0])
	}
	want := []advertising.RemotePage{
		{PageID: "P1", Name: "Loja", PictureURL: "https://pic", WhatsAppNumber: "+55 11 99999-0000", InstagramUserID: "IG1", InstagramUsername: "loja", CanAdvertise: true, LeadTermsAccepted: true},
		{PageID: "P2", Name: "Blog"},
		{PageID: "P3", Name: "Dono", CanAdvertise: true},
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Fatalf("page %d = %+v, want %+v", i, pages[i], want[i])
		}
	}
}

func TestPagingWithoutEndFails(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"data":[],"paging":{"cursors":{"after":"SAME"},"next":"https://graph.facebook.com/next"}}`))

	if _, err := g.ListPages(context.Background(), "tok"); err == nil || !strings.Contains(err.Error(), "did not finish paging") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchLocationsKeepsOnlyTargetableKinds(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[
		{"key":"BR","name":"Brasil","type":"country","country_code":"BR"},
		{"key":"460","name":"São Paulo","type":"region","country_code":"BR"},
		{"key":269969,"name":"Campinas","type":"city","region":"São Paulo","country_code":"BR"},
		{"key":"13000","name":"13000","type":"zip","country_code":"BR"}]}`))

	locations, err := g.SearchLocations(context.Background(), "tok", "sao")
	if err != nil {
		t.Fatal(err)
	}
	q := (*calls)[0].query
	if (*calls)[0].path != "/v26.0/search" || q.Get("type") != "adgeolocation" || q.Get("location_types") != `["country","region","city"]` || q.Get("q") != "sao" || q.Get("limit") != "20" {
		t.Fatalf("query = %v", q)
	}
	want := []advertising.RemoteLocation{
		{Kind: advertising.LocationCountry, Key: "BR", Name: "Brasil", Country: "BR"},
		{Kind: advertising.LocationRegion, Key: "460", Name: "São Paulo", Country: "BR"},
		{Kind: advertising.LocationCity, Key: "269969", Name: "Campinas", Region: "São Paulo", Country: "BR"},
	}
	if len(locations) != len(want) {
		t.Fatalf("locations = %+v", locations)
	}
	for i := range want {
		if locations[i] != want[i] {
			t.Fatalf("location %d = %+v, want %+v", i, locations[i], want[i])
		}
	}
}

func TestGraphVersionOverride(t *testing.T) {
	g, err := newGateway(Config{GraphVersion: "v27.0"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(g.client.BaseURL(), "/v27.0") {
		t.Fatalf("base = %s", g.client.BaseURL())
	}
}

func TestMinimumBudgetsReadsTheAccountFloorAndTheGoalBuckets(t *testing.T) {
	g, calls := gatewayWith(t, func(call recordedCall) (int, string) {
		if strings.HasSuffix(call.path, "/minimum_budgets") {
			return http.StatusOK, `{"data":[
				{"currency":"USD","min_daily_budget_imp":100,"min_daily_budget_video_views":100,"min_daily_budget_high_freq":100,"min_daily_budget_low_freq":4000},
				{"currency":"BRL","min_daily_budget_imp":519,"min_daily_budget_video_views":600,"min_daily_budget_high_freq":519,"min_daily_budget_low_freq":20760}]}`
		}
		return http.StatusOK, `{"currency":"BRL","min_daily_budget":105}`
	})

	got, err := g.MinimumBudgets(context.Background(), "tok", "act_111", 250)
	if err != nil {
		t.Fatal(err)
	}
	want := advertising.MinimumBudgets{Currency: "BRL", Account: 105, Impressions: 519, VideoViews: 600, HighFrequency: 519, LowFrequency: 20760}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls %+v", *calls)
	}
	account, edge := (*calls)[0], (*calls)[1]
	if account.path != "/v26.0/act_111" || !strings.Contains(account.query.Get("fields"), "min_daily_budget") {
		t.Fatalf("account call %+v", account)
	}
	if edge.path != "/v26.0/act_111/minimum_budgets" || edge.query.Get("bid_amount") != "250" {
		t.Fatalf("edge call %+v", edge)
	}
}

func TestMinimumBudgetsWithoutBidOmitsTheBidAndIgnoresOtherCurrencies(t *testing.T) {
	g, calls := gatewayWith(t, func(call recordedCall) (int, string) {
		if strings.HasSuffix(call.path, "/minimum_budgets") {
			return http.StatusOK, `{"data":[{"currency":"USD","min_daily_budget_high_freq":100}]}`
		}
		return http.StatusOK, `{"currency":"BRL","min_daily_budget":105}`
	})

	got, err := g.MinimumBudgets(context.Background(), "tok", "111", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != (advertising.MinimumBudgets{Currency: "BRL", Account: 105}) {
		t.Fatalf("got %+v", got)
	}
	if (*calls)[1].query.Has("bid_amount") {
		t.Fatalf("bid sent: %+v", (*calls)[1])
	}
}

func TestGetBillingAsksForThePaymentMethodOnlyWhenAllowed(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"balance":"1500","is_prepay_account":true,"funding_source_details":{"id":"9001","display_string":"Visa *1234","type":1}}`))

	billing, err := g.GetBilling(context.Background(), "tok", "111", true)
	if err != nil {
		t.Fatal(err)
	}
	want := advertising.RemoteBilling{PaymentMethod: "Visa *1234", Balance: 1500, Prepay: true}
	if *billing != want {
		t.Fatalf("billing = %+v", *billing)
	}
	if call := (*calls)[0]; call.path != "/v26.0/act_111" || call.query.Get("fields") != "balance,is_prepay_account,funding_source_details" {
		t.Fatalf("call = %+v", call)
	}

	if _, err := g.GetBilling(context.Background(), "tok", "111", false); err != nil {
		t.Fatal(err)
	}
	if fields := (*calls)[1].query.Get("fields"); fields != "balance,is_prepay_account" {
		t.Fatalf("fields without the MANAGE task = %s", fields)
	}
}

func TestGetBillingRejectsABalanceThatIsNotAnAmount(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"balance":"12.5"}`))
	if _, err := g.GetBilling(context.Background(), "tok", "111", false); err == nil {
		t.Fatal("expected an error")
	}
}
