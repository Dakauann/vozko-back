package marketing

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
)

const pageTokenReply = `{"access_token":"page-tok","id":"55"}`

func pageAware(respond func(call wireCall) (int, string)) func(wireCall) (int, string) {
	return func(call wireCall) (int, string) {
		if call.method == http.MethodGet && call.path == "/55" && call.query.Get("fields") == "access_token" {
			return http.StatusOK, pageTokenReply
		}
		return respond(call)
	}
}

func afterPageToken(t *testing.T, log *wireLog) []wireCall {
	t.Helper()
	calls := log.all()
	if len(calls) == 0 || calls[0].path != "/55" || calls[0].token != "sys-tok" {
		t.Fatalf("page token lookup = %+v", calls)
	}
	for _, call := range calls[1:] {
		if call.token != "page-tok" {
			t.Fatalf("call without the page token = %+v", call)
		}
	}
	return calls[1:]
}

func TestPageTokenIsResolvedOnceAndReused(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"data":[]}`)))
	for range 3 {
		if _, err := g.ListForms(context.Background(), "sys-tok", "55"); err != nil {
			t.Fatal(err)
		}
	}
	lookups := 0
	for _, call := range log.all() {
		if call.path == "/55" {
			lookups++
		}
	}
	if lookups != 1 {
		t.Fatalf("page token lookups = %d", lookups)
	}
}

func TestPageTokenIsResolvedAgainAfterTenMinutes(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"data":[]}`)))
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	if _, err := g.ListForms(context.Background(), "sys-tok", "55"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := g.ListForms(context.Background(), "sys-tok", "55"); err != nil {
		t.Fatal(err)
	}
	lookups := 0
	for _, call := range log.all() {
		if call.path == "/55" {
			lookups++
		}
	}
	if lookups != 2 {
		t.Fatalf("page token lookups = %d", lookups)
	}
}

func TestPageTokenMustBePresent(t *testing.T) {
	g, _ := wireGateway(t, reply(`{"id":"55"}`))
	if _, err := g.ListForms(context.Background(), "sys-tok", "55"); err == nil {
		t.Fatal("expected an error without a page access token")
	}
}

func TestListFormsMapsQuestionsAndCounts(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"data":[{"id":"700","name":"Out/26","status":"ACTIVE","locale":"pt_BR",
		"questions":[{"key":"full_name","label":"Nome completo","type":"FULL_NAME","id":"q1"},
			{"key":"interesse","label":"Qual plano?","type":"CUSTOM","options":[{"key":"basic","value":"Basico"},{"key":"pro","value":"Pro"}]}],
		"privacy_policy_url":"https://x.com/p","leads_count":12,"created_time":"2026-10-01T12:00:00+0000"}]}`)))

	forms, err := g.ListForms(context.Background(), "sys-tok", "55")
	if err != nil {
		t.Fatal(err)
	}
	calls := afterPageToken(t, log)
	if calls[0].path != "/55/leadgen_forms" || calls[0].query.Get("fields") != leadFormFields {
		t.Fatalf("call = %+v", calls[0])
	}
	f := forms[0]
	if len(forms) != 1 || f.MetaID != "700" || f.PageID != "55" || f.Status != advertising.FormActive || f.Locale != "pt_BR" || f.PrivacyURL != "https://x.com/p" || f.LeadsCount != 12 ||
		f.CreatedTime == nil || !f.CreatedTime.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("form = %+v", f)
	}
	if f.Questions[0].Type != advertising.QuestionFullName || f.Questions[0].Key != "" || f.Questions[0].Label != "Nome completo" {
		t.Fatalf("standard question = %+v", f.Questions[0])
	}
	custom := f.Questions[1]
	if custom.Key != "interesse" || custom.Label != "Qual plano?" || strings.Join(custom.Options, "|") != "Basico|Pro" {
		t.Fatalf("custom question = %+v", custom)
	}
}

func TestCreateFormSerializesTheDraft(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"id":"701"}`)))
	draft := advertising.LeadFormDraft{
		AdAccountID: "77", PageID: "55", Name: "Out/26", Locale: "PT_BR",
		Intro: &advertising.FormIntro{Title: "Fale com a gente", Style: advertising.IntroList, Content: []string{"Rápido", "Sem custo"}},
		Questions: []advertising.FormQuestion{
			{Type: advertising.QuestionFullName},
			{Type: advertising.QuestionPhone},
			{Type: advertising.QuestionCustom, Key: "plano_c", Label: "Qual plano?", Options: []string{"Basico", "Pro"}},
		},
		PrivacyURL: "https://x.com/p", PrivacyText: "Privacidade",
		ThankYouTitle: "Obrigado", ThankYouBody: "Entraremos em contato", ThankYouURL: "https://x.com", ThankYouButtonText: "Ver site",
		HigherIntent: true,
	}
	id, err := g.CreateForm(context.Background(), "sys-tok", draft)
	if err != nil || id != "701" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	call := afterPageToken(t, log)[0]
	if call.method != http.MethodPost || call.path != "/55/leadgen_forms" || call.form.Get("name") != "Out/26" || call.form.Get("locale") != "PT_BR" || call.form.Get("is_optimized_for_quality") != "true" ||
		call.form.Get("follow_up_action_url") != "https://x.com" {
		t.Fatalf("call = %+v", call)
	}
	sameJSON(t, []byte(call.form.Get("questions")), `[{"type":"FULL_NAME"},{"type":"PHONE"},
		{"type":"CUSTOM","key":"plano_c","label":"Qual plano?","options":[{"value":"Basico","key":"Basico"},{"value":"Pro","key":"Pro"}]}]`)
	sameJSON(t, []byte(call.form.Get("privacy_policy")), `{"url":"https://x.com/p","link_text":"Privacidade"}`)
	sameJSON(t, []byte(call.form.Get("thank_you_page")), `{"title":"Obrigado","body":"Entraremos em contato","button_type":"VIEW_WEBSITE","button_text":"Ver site","website_url":"https://x.com"}`)
	sameJSON(t, []byte(call.form.Get("context_card")), `{"title":"Fale com a gente","style":"LIST_STYLE","content":["Rápido","Sem custo"]}`)
}

func TestCreateFormSendsAParagraphIntroAsOneLine(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"id":"703"}`)))
	draft := advertising.LeadFormDraft{
		PageID: "55", Name: "Simples", Locale: "PT_BR", Questions: []advertising.FormQuestion{{Type: advertising.QuestionEmail}}, PrivacyURL: "https://x.com/p",
		Intro:         &advertising.FormIntro{Title: "Sobre", Style: advertising.IntroParagraph, Content: []string{"Um", "Dois"}},
		ThankYouTitle: "Valeu", ThankYouURL: "https://x.com", ThankYouButtonText: "Ver site",
	}
	if _, err := g.CreateForm(context.Background(), "sys-tok", draft); err != nil {
		t.Fatal(err)
	}
	call := afterPageToken(t, log)[0]
	sameJSON(t, []byte(call.form.Get("context_card")), `{"title":"Sobre","style":"PARAGRAPH_STYLE","content":["Um\nDois"]}`)
}

func TestCreateFormOmitsOptionalParts(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"id":"702"}`)))
	draft := advertising.LeadFormDraft{PageID: "55", Name: "Simples", Locale: "PT_BR", Questions: []advertising.FormQuestion{{Type: advertising.QuestionEmail}}, PrivacyURL: "https://x.com/p", ThankYouTitle: "Valeu", ThankYouURL: "https://x.com", ThankYouButtonText: "Ver site"}
	if _, err := g.CreateForm(context.Background(), "sys-tok", draft); err != nil {
		t.Fatal(err)
	}
	call := afterPageToken(t, log)[0]
	if call.form.Has("context_card") || call.form.Has("is_optimized_for_quality") {
		t.Fatalf("form = %v", call.form)
	}
	sameJSON(t, []byte(call.form.Get("thank_you_page")), `{"title":"Valeu","button_type":"VIEW_WEBSITE","button_text":"Ver site","website_url":"https://x.com"}`)
	sameJSON(t, []byte(call.form.Get("privacy_policy")), `{"url":"https://x.com/p"}`)
}

func TestCreateFormSurfacesMetasExplanation(t *testing.T) {
	g, _ := wireGateway(t, pageAware(func(wireCall) (int, string) {
		return http.StatusBadRequest, `{"error":{"message":"Invalid parameter","type":"OAuthException","code":100,"error_subcode":1892085,"error_user_title":"Campos ausentes","error_user_msg":"Campos ausentes: FollowUpActionURL"}}`
	}))
	draft := advertising.LeadFormDraft{PageID: "55", Name: "Simples", Locale: "PT_BR", Questions: []advertising.FormQuestion{{Type: advertising.QuestionEmail}}, PrivacyURL: "https://x.com/p", ThankYouTitle: "Valeu", ThankYouURL: "https://x.com", ThankYouButtonText: "Ver site"}
	_, err := g.CreateForm(context.Background(), "sys-tok", draft)
	if advertising.Classify(err) != advertising.FailureRejected || advertising.Explain(err) != "Campos ausentes: FollowUpActionURL" {
		t.Fatalf("err = %v", err)
	}
}

func TestArchiveFormPostsArchivedStatus(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"success":true}`)))
	if err := g.ArchiveForm(context.Background(), "sys-tok", "55", "700"); err != nil {
		t.Fatal(err)
	}
	call := afterPageToken(t, log)[0]
	if call.method != http.MethodPost || call.path != "/700" || call.form.Get("status") != "ARCHIVED" {
		t.Fatalf("call = %+v", call)
	}
	refused, _ := wireGateway(t, pageAware(reply(`{"success":false}`)))
	if err := refused.ArchiveForm(context.Background(), "sys-tok", "55", "700"); err == nil {
		t.Fatal("expected an error when meta does not acknowledge")
	}
}

func TestListLeadsFiltersBySinceAndJoinsAnswers(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"data":[{"id":"9001","created_time":"2026-10-01T12:00:00+0000","ad_id":"33","form_id":"700",
		"field_data":[{"name":"full_name","values":["Maria Silva"]},{"name":"interesses","values":["pro","basic"]}]}]}`)))
	since := time.Unix(1790000000, 0)
	leads, err := g.ListLeads(context.Background(), "sys-tok", "55", "700", since)
	if err != nil {
		t.Fatal(err)
	}
	call := afterPageToken(t, log)[0]
	if call.path != "/700/leads" || call.query.Get("fields") != leadFields {
		t.Fatalf("call = %+v", call)
	}
	sameJSON(t, []byte(call.query.Get("filtering")), `[{"field":"time_created","operator":"GREATER_THAN","value":1790000000}]`)
	lead := leads[0]
	if len(leads) != 1 || lead.MetaID != "9001" || lead.FormMetaID != "700" || lead.AdMetaID != "33" || lead.PageID != "55" ||
		lead.Answers["full_name"] != "Maria Silva" || lead.Answers["interesses"] != "pro, basic" || !lead.CreatedTime.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("lead = %+v", lead)
	}
}

func TestListLeadsWithoutSinceSendsNoFilter(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"data":[]}`)))
	if _, err := g.ListLeads(context.Background(), "sys-tok", "55", "700", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if call := afterPageToken(t, log)[0]; call.query.Has("filtering") {
		t.Fatalf("call = %+v", call)
	}
}

func TestGetLeadMapsOneLeadAndNeedsACreatedTime(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"id":"9001","created_time":"2026-10-01T12:00:00+0000","form_id":"700","field_data":[{"name":"email","values":["m@x.com"]}]}`)))
	lead, err := g.GetLead(context.Background(), "sys-tok", "55", "9001")
	if err != nil {
		t.Fatal(err)
	}
	if call := afterPageToken(t, log)[0]; call.path != "/9001" || call.query.Get("fields") != leadFields {
		t.Fatalf("call = %+v", call)
	}
	if lead.Contact().Email != "m@x.com" || lead.FormMetaID != "700" {
		t.Fatalf("lead = %+v", lead)
	}
	broken, _ := wireGateway(t, pageAware(reply(`{"id":"9001","form_id":"700"}`)))
	if _, err := broken.GetLead(context.Background(), "sys-tok", "55", "9001"); err == nil {
		t.Fatal("expected an error without created_time")
	}
}

func TestSubscribeLeadgenKeepsExistingFields(t *testing.T) {
	g, log := wireGateway(t, pageAware(func(call wireCall) (int, string) {
		if call.method == http.MethodGet {
			return http.StatusOK, `{"data":[{"id":"app","name":"Vozko","subscribed_fields":["messages","messaging_postbacks","feed"]}]}`
		}
		return http.StatusOK, `{"success":true}`
	}))
	if err := g.SubscribeLeadgen(context.Background(), "sys-tok", "55"); err != nil {
		t.Fatal(err)
	}
	calls := afterPageToken(t, log)
	if len(calls) != 2 || calls[0].path != "/55/subscribed_apps" || calls[1].method != http.MethodPost || calls[1].path != "/55/subscribed_apps" {
		t.Fatalf("calls = %+v", calls)
	}
	if got := calls[1].form.Get("subscribed_fields"); got != "messages,messaging_postbacks,feed,leadgen" {
		t.Fatalf("subscribed_fields = %q", got)
	}
}

func TestSubscribeLeadgenSubscribesAFreshPage(t *testing.T) {
	g, log := wireGateway(t, pageAware(func(call wireCall) (int, string) {
		if call.method == http.MethodGet {
			return http.StatusOK, `{"data":[]}`
		}
		return http.StatusOK, `{"success":true}`
	}))
	if err := g.SubscribeLeadgen(context.Background(), "sys-tok", "55"); err != nil {
		t.Fatal(err)
	}
	if got := afterPageToken(t, log)[1].form.Get("subscribed_fields"); got != "leadgen" {
		t.Fatalf("subscribed_fields = %q", got)
	}
}

func TestSubscribeLeadgenSkipsWhenAlreadySubscribed(t *testing.T) {
	g, log := wireGateway(t, pageAware(reply(`{"data":[{"id":"app","subscribed_fields":["messages","leadgen"]}]}`)))
	if err := g.SubscribeLeadgen(context.Background(), "sys-tok", "55"); err != nil {
		t.Fatal(err)
	}
	if calls := afterPageToken(t, log); len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestSubscribeLeadgenRefusesAmbiguousOrUnacknowledgedSubscriptions(t *testing.T) {
	ambiguous, log := wireGateway(t, pageAware(reply(`{"data":[{"id":"a","subscribed_fields":["feed"]},{"id":"b","subscribed_fields":["messages"]}]}`)))
	if err := ambiguous.SubscribeLeadgen(context.Background(), "sys-tok", "55"); err == nil {
		t.Fatal("expected an error for more than one app")
	}
	if calls := afterPageToken(t, log); len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	refused, _ := wireGateway(t, pageAware(func(call wireCall) (int, string) {
		if call.method == http.MethodGet {
			return http.StatusOK, `{"data":[]}`
		}
		return http.StatusOK, `{"success":false}`
	}))
	if err := refused.SubscribeLeadgen(context.Background(), "sys-tok", "55"); err == nil {
		t.Fatal("expected an error when meta does not acknowledge")
	}
}

func TestListFormsReadsTheIntroAndTheThankYouScreen(t *testing.T) {
	g, _ := wireGateway(t, pageAware(reply(`{"data":[{"id":"701","name":"Vozko","status":"ACTIVE","leads_count":0,
		"context_card":{"id":"9","title":"Conheça a plataforma","style":"LIST_STYLE","content":["Todos os canais","IA que responde"]},
		"thank_you_page":{"id":"8","title":"Recebemos seu contato","body":"Em breve falamos com você","button_text":"Falar no WhatsApp","website_url":"https://wa.me/5511900000000"},
		"is_optimized_for_quality":true}]}`)))

	forms, err := g.ListForms(context.Background(), "sys-tok", "55")
	if err != nil {
		t.Fatal(err)
	}
	f := forms[0]
	if f.Intro == nil || f.Intro.Title != "Conheça a plataforma" || f.Intro.Style != advertising.IntroList || strings.Join(f.Intro.Content, "|") != "Todos os canais|IA que responde" {
		t.Fatalf("intro = %+v", f.Intro)
	}
	if f.ThankYouTitle != "Recebemos seu contato" || f.ThankYouBody != "Em breve falamos com você" || f.ThankYouButtonText != "Falar no WhatsApp" ||
		f.ThankYouURL != "https://wa.me/5511900000000" || !f.HigherIntent {
		t.Fatalf("form = %+v", f)
	}
}

func TestListFormsLeavesAnUnknownIntroStyleOut(t *testing.T) {
	g, _ := wireGateway(t, pageAware(reply(`{"data":[{"id":"702","name":"Sem intro","status":"ACTIVE","leads_count":0,
		"context_card":{"title":"Algo","style":"NEW_STYLE","content":["x"]}}]}`)))
	forms, err := g.ListForms(context.Background(), "sys-tok", "55")
	if err != nil || forms[0].Intro != nil {
		t.Fatalf("forms %+v err %v", forms, err)
	}
}
