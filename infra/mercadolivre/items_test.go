package mercadolivre

import (
	"context"
	"net/http"
	"strings"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

func TestSearchSellerItemsWithOffset(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "items_search_doc_example.json")))
	limiters := newFakeLimiters()
	s, err := NewItemService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.SearchSellerItems(context.Background(), sellerCreds, mldomain.ItemSearch{Status: "active", Offset: 50, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodGet || r.Path != "/users/415458330/items/search" ||
		r.Query.Get("status") != "active" || r.Query.Get("offset") != "50" || r.Query.Get("limit") != "100" {
		t.Fatalf("%s %s %v", r.Method, r.Path, r.Query)
	}
	assertBearer(t, r)
	if keys := limiters.keys(); keys[1] != "ml:app:items" {
		t.Fatalf("limiter keys = %v", keys)
	}
	if len(page.IDs) != 1 || page.IDs[0] != "MLA844702264" || page.Total != 1 || page.Limit != 50 {
		t.Fatalf("page = %+v", page)
	}
}

func TestSearchSellerItemsWithScan(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"seller_id":"415458330","paging":{"limit":100,"total":2500},"results":["MLB1","MLB2"],"scroll_id":"YXBp=="}`))
	s, _ := NewItemService(newTestClient(t, fs, newFakeLimiters()))
	page, err := s.SearchSellerItems(context.Background(), sellerCreds, mldomain.ItemSearch{Scan: true, ScrollID: "prev==", Offset: 30})
	if err != nil {
		t.Fatal(err)
	}
	q := fs.last().Query
	if q.Get("search_type") != "scan" || q.Get("scroll_id") != "prev==" {
		t.Fatalf("query = %v", q)
	}
	if _, ok := q["offset"]; ok {
		t.Fatalf("scan must not mix offset with scroll: %v", q)
	}
	if page.ScrollID != "YXBp==" || len(page.IDs) != 2 {
		t.Fatalf("page = %+v", page)
	}
}

func TestGetItemsUsesTheBulkEndpoint(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "items_bulk_from_multiget_doc_example.json")))
	s, _ := NewItemService(newTestClient(t, fs, newFakeLimiters()))
	items, failures, err := s.GetItems(context.Background(), sellerCreds, []string{"MLB599260060", "MLB594239600", "MLB111", "MLB599260060"})
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Path != "/items/bulk" || r.Query.Get("ids") != "MLB599260060,MLB594239600,MLB111" {
		t.Fatalf("%s %v", r.Path, r.Query)
	}
	if len(items) != 2 || items[0].ID != "MLB599260060" || items[0].Title != "Item De Test - Por Favor No Ofertar" ||
		items[0].SellerID != "303888594" || items[0].Status != "active" || items[0].SiteID != "MLB" ||
		!strings.HasPrefix(items[0].Thumbnail, "http") || items[1].Status != "paused" {
		t.Fatalf("items = %+v %+v", items[0], items[1])
	}
	if len(failures) != 1 || failures[0].ID != "MLB111" || failures[0].StatusCode != 404 {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestGetItemsRefusals(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `[]`))
	s, _ := NewItemService(newTestClient(t, fs, newFakeLimiters()))
	many := make([]string, 21)
	for i := range many {
		many[i] = "MLB" + strings.Repeat("1", i+1)
	}
	for _, ids := range [][]string{nil, {}, {"MLA1"}, {"MLB1,MLB2"}, many} {
		if _, _, err := s.GetItems(context.Background(), sellerCreds, ids); err == nil {
			t.Errorf("GetItems(%v) accepted", ids)
		}
	}
	if fs.count() != 0 {
		t.Fatal("refusals must not reach Mercado Livre")
	}
}

func TestUsers(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "users_me_doc_example_trimmed.json")))
	limiters := newFakeLimiters()
	s, err := NewUserService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	me, err := s.Me(context.Background(), sellerCreds)
	if err != nil {
		t.Fatal(err)
	}
	if fs.last().Path != "/users/me" {
		t.Fatalf("path = %s", fs.last().Path)
	}
	assertBearer(t, fs.last())
	if me.ID != "202593498" || me.Nickname != "TETE2870021" || me.SiteID != "MLA" || me.CountryID != "AR" ||
		me.UserType != "real_estate_agency" || len(me.Tags) != 3 {
		t.Fatalf("user = %+v", me)
	}
	if me.FirstName != "Test" || me.LastName != "Test" || me.BrandName != "" || me.CorporateName != "" {
		t.Fatalf("names without a company block = %+v", me)
	}
	if _, err := s.GetUser(context.Background(), sellerCreds, "1944693439"); err != nil {
		t.Fatal(err)
	}
	if fs.last().Path != "/users/1944693439" {
		t.Fatalf("path = %s", fs.last().Path)
	}
	if _, err := s.GetUser(context.Background(), sellerCreds, "me"); err == nil {
		t.Fatal("GetUser must take a numeric id")
	}
}

func TestUsersDecodeTheCompanyNames(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "users_me_constructed_with_company.json")))
	s, _ := NewUserService(newTestClient(t, fs, newFakeLimiters()))
	for _, fetch := range []func() (*mldomain.User, error){
		func() (*mldomain.User, error) { return s.Me(context.Background(), sellerCreds) },
		func() (*mldomain.User, error) { return s.GetUser(context.Background(), sellerCreds, "202593498") },
	} {
		user, err := fetch()
		if err != nil {
			t.Fatal(err)
		}
		if user.Nickname != "DGCAFEDHB50478" || user.FirstName != "Ana" || user.LastName != "Souza" ||
			user.BrandName != "Café do Vale" || user.CorporateName != "Vale Cafés Ltda" {
			t.Fatalf("user = %+v", user)
		}
	}
}

func TestUsersRefuseAnAnswerWithoutAnID(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"nickname":"x"}`))
	s, _ := NewUserService(newTestClient(t, fs, newFakeLimiters()))
	if _, err := s.Me(context.Background(), sellerCreds); err == nil {
		t.Fatal("a user without id must be an error")
	}
}
