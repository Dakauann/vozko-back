package formtools

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	"vozko/usecases/copilot/copilottools/tooltest"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

func adminContext() copilot.Context {
	cc := tooltest.Member()
	cc.SystemAdmin = true
	return cc
}

func TestFormToolsActAsTheSessionPerson(t *testing.T) {
	forms := &fakeForms{}

	NewListWhatsAppFormsTool(deps(forms)).Execute(context.Background(), adminContext(), map[string]interface{}{})

	if forms.actor.UserID != "u-1" || forms.actor.WorkspaceID != "ws-1" || !forms.actor.IsAdmin {
		t.Fatalf("actor = %+v", forms.actor)
	}
}

func TestListWhatsAppFormsShowsEachFormWithItsAccountsAndCounts(t *testing.T) {
	last := at("2026-10-18 14:30")
	forms := &fakeForms{summaries: []wf.FormSummary{{
		Form:         wf.Form{ID: knownForm, Name: "Captação", PublishedVersionID: "v-1"},
		Publications: []wf.PublicationBrief{{MetaWABAID: knownWABA, Status: wf.PublicationPublished}},
		Sent:         12, Responses: 5, LastResponseAt: &last,
	}}}

	res := NewListWhatsAppFormsTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"search": " capt ", "include_archived": true})

	if res.Status != copilot.StatusOK || forms.searched != "capt" || !forms.archived {
		t.Fatalf("res = %+v, searched %q archived %v", res, forms.searched, forms.archived)
	}
	listed := tooltest.DataOf(res)["forms"].([]map[string]interface{})
	row := listed[0]
	if row["form_id"] != knownForm || row["name"] != "Captação" || row["sent"] != int64(12) || row["responses"] != int64(5) || row["published"] != true {
		t.Fatalf("row = %+v", row)
	}
	accounts := row["accounts"].([]map[string]interface{})
	if len(accounts) != 1 || accounts[0]["account"] != knownWABA || accounts[0]["status"] != "published" {
		t.Fatalf("accounts = %+v", accounts)
	}
	if row["last_response"] != "2026-10-18T14:30:00Z" {
		t.Fatalf("last response = %v", row["last_response"])
	}
}

func TestListWhatsAppFormsIsARead(t *testing.T) {
	tool := NewListWhatsAppFormsTool(deps(&fakeForms{}))
	if m := tool.Meta(); m.Mutating || m.Resource != "whatsapp_forms" || m.Action != "read" {
		t.Fatalf("meta = %+v", m)
	}
}

func TestAFormIsFoundByIdOrByItsExactName(t *testing.T) {
	detail := knownDetail()
	forms := formsWith(detail)
	forms.summaries = []wf.FormSummary{{Form: wf.Form{ID: otherForm, Name: "Captação de lead"}}, {Form: wf.Form{ID: knownForm, Name: "Captação"}}}
	tool := NewWhatsAppFormResultsTool(deps(forms))

	res := tool.Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_name": "captação"})
	if res.Status != copilot.StatusOK || forms.searched != "captação" || !forms.called("Stats:"+knownForm) {
		t.Fatalf("res = %+v, calls %v", res, forms.calls)
	}

	missing := tool.Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_name": "Orçamento"})
	if missing.Status == copilot.StatusOK || !strings.Contains(missing.Message, "list_whatsapp_forms") {
		t.Fatalf("missing = %+v", missing)
	}

	none := tool.Execute(context.Background(), tooltest.Member(), map[string]interface{}{})
	if none.Status == copilot.StatusOK || !strings.Contains(none.Message, "form_id") {
		t.Fatalf("none = %+v", none)
	}

	invented := tool.Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": "form-123"})
	if invented.Status == copilot.StatusOK || !strings.Contains(invented.Message, "invente") {
		t.Fatalf("invented = %+v", invented)
	}
}

func TestWhatsAppFormResultsReportsStatsAndEveryChoiceField(t *testing.T) {
	median := 90.0
	forms := formsWith(knownDetail())
	forms.stats = wf.Stats{
		Sends: map[wf.SendStatus]int64{wf.SendSent: 10, wf.SendResponded: 4}, Delivered: 13, Read: 9, Responses: 4, MedianSecondsToRespond: &median,
		ByOrigin: []wf.OriginStat{{Origin: wf.OriginInbox, Sent: 14, Responses: 4}},
		ByDay:    []wf.DayStat{{Day: "2026-10-17", Sent: 8, Responses: 2}, {Day: "2026-10-18", Sent: 6, Responses: 2}},
	}

	res := NewWhatsAppFormResultsTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm, "from": "2026-10-01", "to": "2026-10-18"})

	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	if forms.period.From == nil || !forms.period.From.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || forms.period.To == nil || forms.period.To.Day() != 18 || forms.period.To.Hour() != 23 {
		t.Fatalf("period = %+v", forms.period)
	}
	if len(forms.distributed) != 1 || forms.distributed[0] != "f_interesse" {
		t.Fatalf("only choice fields are distributed: %v", forms.distributed)
	}
	data := tooltest.DataOf(res)
	if data["responses"] != int64(4) || data["delivered"] != int64(13) || data["read"] != int64(9) || data["median_seconds_to_respond"] != 90.0 {
		t.Fatalf("data = %+v", data)
	}
	if sends := data["sends"].(map[string]int64); sends["sent"] != 10 || sends["responded"] != 4 {
		t.Fatalf("sends = %+v", data["sends"])
	}
	choices := data["choice_fields"].([]map[string]interface{})
	options := choices[0]["options"].([]map[string]interface{})
	if choices[0]["question"] != "O que você procura?" || options[0]["option"] != "Produtos" || options[0]["responses"] != int64(7) || options[1]["option"] != "Vendas" {
		t.Fatalf("choices = %+v", choices)
	}
	if _, ok := data["by_day"]; !ok {
		t.Fatalf("the daily series must be a dataset for render_chart: %+v", data)
	}
}

func TestWhatsAppFormResultsRefusesAnUnreadablePeriodBeforeAsking(t *testing.T) {
	forms := formsWith(knownDetail())

	res := NewWhatsAppFormResultsTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm, "from": "ontem"})

	if res.Status == copilot.StatusOK || forms.called("Stats:"+knownForm) || !strings.Contains(res.Message, "AAAA-MM-DD") {
		t.Fatalf("res = %+v, calls %v", res, forms.calls)
	}
}

func TestWhatsAppFormResultsNeedsTheResponsesCapability(t *testing.T) {
	tool := NewWhatsAppFormResultsTool(deps(&fakeForms{}))
	if m := tool.Meta(); m.Mutating || m.Resource != "whatsapp_form_responses" || m.Action != "read" {
		t.Fatalf("meta = %+v", m)
	}
	if !requires(tool, "whatsapp_forms:read", "whatsapp_form_responses:read") {
		t.Fatalf("requires %v", requirementKeys(tool))
	}
}

func TestWhatsAppFormResponsesListsMaskedAnswersWithLeadNamesAndACursor(t *testing.T) {
	forms := formsWith(knownDetail())
	forms.responses = wf.Page[whatsappform_usecase.ResponseRow]{Next: "cursor-2", Items: []whatsappform_usecase.ResponseRow{{
		LeadName: "Maria",
		Response: wf.Response{
			ID: "resp-1", FormID: knownForm, VersionID: "v-1", EntryID: knownEntry, EntryType: "whatsapp", ContactNumber: "5584994409624",
			SubmittedAt: at("2026-10-18 09:15"),
			Answers: wf.Answers{Values: map[string]wf.Answer{
				"f_nome":      {Key: "f_nome", Kind: wf.FieldShortText, Text: "Maria Souza"},
				"f_cpf":       {Key: "f_cpf", Kind: wf.FieldShortText, Text: "••••", Masked: true},
				"f_interesse": {Key: "f_interesse", Kind: wf.FieldRadio, OptionIDs: []string{"o_ab"}},
			}},
		},
	}}}

	res := NewWhatsAppFormResponsesTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm, "after": cursorAfter(), "search": "maria"})

	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	if forms.responsesQ.FormID != knownForm || forms.responsesQ.After != cursorAfter() || forms.responsesQ.Search != "maria" || forms.responsesQ.Limit != responsesPageSize {
		t.Fatalf("query = %+v", forms.responsesQ)
	}
	data := tooltest.DataOf(res)
	if data["next"] != "cursor-2" {
		t.Fatalf("next = %v", data["next"])
	}
	row := data["responses"].([]map[string]interface{})[0]
	if row["response_id"] != "resp-1" || row["lead_name"] != "Maria" || row["entry_id"] != knownEntry || row["submitted_at"] != "2026-10-18T09:15:00Z" {
		t.Fatalf("row = %+v", row)
	}
	answers := row["answers"].([]map[string]interface{})
	got := map[string]string{}
	for _, a := range answers {
		got[a["question"].(string)] = a["answer"].(string)
	}
	if got["Nome"] != "Maria Souza" || got["CPF"] != "••••" || got["O que você procura?"] != "Vendas" {
		t.Fatalf("answers = %+v", answers)
	}
}

func TestWhatsAppFormResponsesSaysWhenThereAreNoMorePages(t *testing.T) {
	forms := formsWith(knownDetail())

	res := NewWhatsAppFormResponsesTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	if _, paged := tooltest.DataOf(res)["next"]; paged {
		t.Fatalf("no cursor without more pages: %+v", tooltest.DataOf(res))
	}
}

func cursorAfter() string {
	return shared.Keyset{At: at("2026-10-18 10:00"), ID: "resp-0"}.Encode()
}
