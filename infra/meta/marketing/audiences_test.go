package marketing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/infra/meta/metatest"
)

type wireCall struct {
	method string
	path   string
	token  string
	query  url.Values
	form   url.Values
	body   []byte
}

func (c wireCall) jsonField(t *testing.T, field string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(c.form.Get(field)), out); err != nil {
		t.Fatalf("form field %s = %q: %v", field, c.form.Get(field), err)
	}
}

type wireLog struct {
	mu    sync.Mutex
	calls []wireCall
}

func (l *wireLog) all() []wireCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]wireCall(nil), l.calls...)
}

func wireGateway(t *testing.T, respond func(call wireCall) (int, string)) (*Gateway, *wireLog) {
	t.Helper()
	log := &wireLog{}
	client := metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		call := wireCall{
			method: r.Method,
			path:   strings.TrimPrefix(r.URL.Path, "/"+DefaultGraphVersion),
			token:  strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
			query:  r.URL.Query(),
			form:   url.Values{},
			body:   raw,
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			call.form, _ = url.ParseQuery(string(raw))
		}
		log.mu.Lock()
		log.calls = append(log.calls, call)
		log.mu.Unlock()
		status, body := respond(call)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	g, err := newGateway(Config{AppSecret: "secret", HTTPClient: client}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return g, log
}

func reply(body string) func(wireCall) (int, string) {
	return func(wireCall) (int, string) { return http.StatusOK, body }
}

func sameJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("want %s: %v", want, err)
	}
	ga, _ := json.Marshal(a)
	wb, _ := json.Marshal(b)
	if string(ga) != string(wb) {
		t.Fatalf("json = %s\nwant %s", ga, wb)
	}
}

func TestCustomAudienceTermsNeedTheCustomAudienceKeySetToOne(t *testing.T) {
	cases := map[string]bool{
		`{"tos_accepted":{"custom_audience_tos":1}}`:                               true,
		`{"tos_accepted":{"custom_audience_tos":"1","web_custom_audience_tos":1}}`: true,
		`{"tos_accepted":{"web_custom_audience_tos":1}}`:                           false,
		`{"tos_accepted":{"custom_audience_tos":0}}`:                               false,
		`{"id":"act_1"}`: false,
	}
	for body, want := range cases {
		g, log := wireGateway(t, reply(body))
		got, err := g.CustomAudienceTermsAccepted(context.Background(), "tok", "act_77")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s accepted = %v, want %v", body, got, want)
		}
		call := log.all()[0]
		if call.method != http.MethodGet || call.path != "/act_77" || call.query.Get("fields") != "tos_accepted" {
			t.Fatalf("call = %+v", call)
		}
	}
}

func TestListAudiencesPagesAndMapsKindsCountsAndLookalikes(t *testing.T) {
	g, log := wireGateway(t, func(call wireCall) (int, string) {
		if call.query.Get("after") == "" {
			return http.StatusOK, `{"data":[
				{"id":"1","name":"Clientes","description":"crm","subtype":"CUSTOM","approximate_count_lower_bound":1000,"approximate_count_upper_bound":"1200",
				 "delivery_status":{"code":200,"description":"ok"},"operation_status":{"code":200,"description":"Normal"},"time_created":1790000000,"time_updated":"1790000600"},
				{"id":"2","name":"LAL","subtype":"LOOKALIKE","approximate_count_lower_bound":-1,"approximate_count_upper_bound":-1,
				 "lookalike_spec":{"ratio":0.03,"country":"BR","origin":[{"id":"1","name":"Clientes","type":"custom_audience"}]}}
			],"paging":{"cursors":{"after":"A"},"next":"https://graph.facebook.com/next"}}`
		}
		return http.StatusOK, `{"data":[{"id":"3","name":"Site","subtype":"WEBSITE","retention_days":30},{"id":"4","name":"Eng","subtype":"ENGAGEMENT"},{"id":"5","name":"Outro","subtype":"VIDEO"}],"paging":{}}`
	})

	audiences, err := g.ListAudiences(context.Background(), "tok", "77")
	if err != nil {
		t.Fatal(err)
	}
	calls := log.all()
	if len(calls) != 2 || calls[0].path != "/act_77/customaudiences" || calls[1].query.Get("after") != "A" || calls[0].query.Get("fields") != audienceFields {
		t.Fatalf("calls = %+v", calls)
	}
	if len(audiences) != 5 {
		t.Fatalf("audiences = %+v", audiences)
	}
	created := time.Unix(1790000000, 0).UTC()
	first := audiences[0]
	if first.Kind != advertising.AudienceCustomerList || first.ApproxLower != 1000 || first.ApproxUpper != 1200 || !first.Ready() ||
		first.OperationDescription != "Normal" || first.CreatedTime == nil || !first.CreatedTime.Equal(created) || first.UpdatedTime == nil || first.UpdatedTime.Sub(created) != 10*time.Minute {
		t.Fatalf("first = %+v", first)
	}
	lal := audiences[1]
	if lal.Kind != advertising.AudienceLookalike || lal.ApproxLower != -1 || lal.ApproxUpper != -1 || lal.OriginAudienceID != "1" || lal.LookalikeRatio != 0.03 || lal.LookalikeCountry != "BR" {
		t.Fatalf("lookalike = %+v", lal)
	}
	if audiences[2].Kind != advertising.AudienceWebsite || audiences[2].RetentionDays != 30 || audiences[2].ApproxLower != -1 {
		t.Fatalf("website = %+v", audiences[2])
	}
	if audiences[3].Kind != advertising.AudienceEngagement || audiences[4].Kind != advertising.AudienceOther {
		t.Fatalf("kinds = %+v", audiences[3:])
	}
}

func TestListAudiencesRejectsUnparseableCounts(t *testing.T) {
	g, _ := wireGateway(t, reply(`{"data":[{"id":"1","subtype":"CUSTOM","approximate_count_lower_bound":"many"}]}`))
	if _, err := g.ListAudiences(context.Background(), "tok", "77"); err == nil {
		t.Fatal("expected an error for a non numeric count")
	}
}

func TestCreateCustomerListPostsUserProvidedCustomAudience(t *testing.T) {
	g, log := wireGateway(t, reply(`{"id":"900"}`))
	id, err := g.CreateCustomerList(context.Background(), "tok", "act_77", "Clientes", "vindos do crm")
	if err != nil || id != "900" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	call := log.all()[0]
	want := url.Values{"name": {"Clientes"}, "description": {"vindos do crm"}, "subtype": {"CUSTOM"}, "customer_file_source": {"USER_PROVIDED_ONLY"}}
	if call.method != http.MethodPost || call.path != "/act_77/customaudiences" || call.form.Encode() != want.Encode() {
		t.Fatalf("call = %+v", call)
	}
}

func TestCreateCustomerListNeedsAnID(t *testing.T) {
	g, _ := wireGateway(t, reply(`{}`))
	if _, err := g.CreateCustomerList(context.Background(), "tok", "77", "Clientes", ""); err == nil {
		t.Fatal("expected an error when meta returns no id")
	}
}

func hashedBatch(rows int) advertising.HashedCustomers {
	batch := advertising.HashedCustomers{Keys: []advertising.MatchKey{advertising.MatchEmail, advertising.MatchPhone}}
	for range rows {
		batch.Rows = append(batch.Rows, []string{"e", ""})
	}
	return batch
}

func TestAddCustomersSendsSessionAndPayload(t *testing.T) {
	g, log := wireGateway(t, reply(`{"audience_id":"900","session_id":42,"num_received":2,"num_invalid_entries":0}`))
	batch := advertising.HashedCustomers{
		Keys: []advertising.MatchKey{advertising.MatchEmail, advertising.MatchPhone},
		Rows: [][]string{{"aa", "bb"}, {"", "cc"}},
	}
	session := advertising.CustomerSession{ID: 42, BatchSeq: 1, LastBatch: true, TotalRows: 2}
	if err := g.AddCustomers(context.Background(), "tok", "900", batch, session); err != nil {
		t.Fatal(err)
	}
	call := log.all()[0]
	if call.method != http.MethodPost || call.path != "/900/users" {
		t.Fatalf("call = %+v", call)
	}
	sameJSON(t, call.body, `{"session":{"session_id":42,"batch_seq":1,"last_batch_flag":true,"estimated_num_total":2},
		"payload":{"schema":["EMAIL","PHONE"],"data":[["aa","bb"],["","cc"]]}}`)
}

func TestAddCustomersFailsWhenMetaReceivedFewerRows(t *testing.T) {
	g, _ := wireGateway(t, reply(`{"num_received":1}`))
	err := g.AddCustomers(context.Background(), "tok", "900", hashedBatch(2), advertising.CustomerSession{ID: 1, BatchSeq: 1})
	if err == nil || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddCustomersExpectsTheSessionTotalSoFar(t *testing.T) {
	g, _ := wireGateway(t, reply(`{"num_received":5}`))
	session := advertising.CustomerSession{ID: 1, BatchSeq: 2, LastBatch: true, TotalRows: 5, SentRows: 3}
	if err := g.AddCustomers(context.Background(), "tok", "900", hashedBatch(2), session); err != nil {
		t.Fatal(err)
	}
	short, _ := wireGateway(t, reply(`{"num_received":2}`))
	if err := short.AddCustomers(context.Background(), "tok", "900", hashedBatch(2), session); err == nil || !strings.Contains(err.Error(), "2 of 5") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddCustomersFailsWithoutAReceivedCount(t *testing.T) {
	g, _ := wireGateway(t, reply(`{}`))
	if err := g.AddCustomers(context.Background(), "tok", "900", hashedBatch(1), advertising.CustomerSession{ID: 1, BatchSeq: 1}); err == nil {
		t.Fatal("expected an error without num_received")
	}
}

func TestAddCustomersRefusesBadBatchesBeforeCalling(t *testing.T) {
	g, log := wireGateway(t, reply(`{"num_received":0}`))
	ragged := hashedBatch(1)
	ragged.Rows[0] = []string{"only one"}
	for name, batch := range map[string]advertising.HashedCustomers{
		"too many": hashedBatch(advertising.CustomerBatchSize() + 1),
		"empty":    hashedBatch(0),
		"ragged":   ragged,
	} {
		if err := g.AddCustomers(context.Background(), "tok", "900", batch, advertising.CustomerSession{ID: 1, BatchSeq: 1}); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if len(log.all()) != 0 {
		t.Fatalf("calls = %+v", log.all())
	}
}

func TestAddCustomersAcceptsAFullBatch(t *testing.T) {
	size := advertising.CustomerBatchSize()
	g, _ := wireGateway(t, reply(`{"num_received":`+jsonInt(size)+`}`))
	if err := g.AddCustomers(context.Background(), "tok", "900", hashedBatch(size), advertising.CustomerSession{ID: 1, BatchSeq: 1}); err != nil {
		t.Fatal(err)
	}
}

func jsonInt(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func TestCreateLookalikeSendsRatioAndCountry(t *testing.T) {
	g, log := wireGateway(t, reply(`{"id":"901"}`))
	id, err := g.CreateLookalike(context.Background(), "tok", "77", advertising.LookalikeDraft{AdAccountID: "77", Name: "LAL 2%", OriginAudienceID: "900", Percent: 2, Country: "BR"})
	if err != nil || id != "901" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	call := log.all()[0]
	if call.path != "/act_77/customaudiences" || call.form.Get("subtype") != "LOOKALIKE" || call.form.Get("origin_audience_id") != "900" || call.form.Get("name") != "LAL 2%" {
		t.Fatalf("call = %+v", call)
	}
	sameJSON(t, []byte(call.form.Get("lookalike_spec")), `{"ratio":0.02,"country":"BR"}`)
}

func TestDeleteAudienceRequiresSuccess(t *testing.T) {
	g, log := wireGateway(t, reply(`{"success":true}`))
	if err := g.DeleteAudience(context.Background(), "tok", "900"); err != nil {
		t.Fatal(err)
	}
	if call := log.all()[0]; call.method != http.MethodDelete || call.path != "/900" {
		t.Fatalf("call = %+v", call)
	}
	refused, _ := wireGateway(t, reply(`{"success":false}`))
	if err := refused.DeleteAudience(context.Background(), "tok", "900"); err == nil {
		t.Fatal("expected an error when meta does not acknowledge")
	}
}

func TestAudienceCallsSurfaceGraphErrors(t *testing.T) {
	g, _ := wireGateway(t, func(wireCall) (int, string) {
		return http.StatusBadRequest, `{"error":{"code":2650,"error_subcode":1870145,"message":"update in progress"}}`
	})
	err := g.AddCustomers(context.Background(), "tok", "900", hashedBatch(1), advertising.CustomerSession{ID: 1, BatchSeq: 1})
	if advertising.Classify(err) != advertising.FailureRejected {
		t.Fatalf("err = %v", err)
	}
}
