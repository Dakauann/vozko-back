package campaignaudience_usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

var ctx = context.Background()

func everyoneSelection() *selection.Selection {
	s := selection.Selection{Mode: selection.ModeEveryone, Filter: &crmfilter.Filter{}}
	return &s
}

func confirmed(s *selection.Selection, count int) *selection.Selection {
	c := *s
	c.ExpectedCount = count
	c.Fingerprint = selection.Fingerprint(c.EffectiveFilter())
	return &c
}

func (w *world) upload(t *testing.T, channel campaignaudience.Channel, text string, view View) Preview {
	t.Helper()
	preview, err := w.service.Upload(ctx, testActor, channel, Upload{FileName: "lista.csv", Data: []byte(text)}, view)
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestCountOfLeadsUsesTheChannelRulesAndTellsWhatACreateMustConfirm(t *testing.T) {
	w := newWorld(t)
	w.selections.ids = []string{"a", "b", "c", "gone"}
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName}, {Key: "2"}}
	w.channel.judge = fakeJudge{leads: map[string]campaignaudience.Candidate{
		"a": {HasContact: true, Values: []string{"Ana", "Centro"}},
		"b": {HasContact: true, Refusal: string(campaign.SkipBlocked)},
		"c": {HasContact: true, Values: []string{"", ""}},
	}}
	spec := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: everyoneSelection(), Variables: []campaignaudience.Variable{
		{Key: "1", Missing: campaignaudience.MissingText, Text: "cliente"},
		{Key: "2", Source: campaign.BindDistrict, Missing: campaignaudience.MissingExclude},
	}}
	count, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if count.Matched != 3 || count.Sendable == nil || *count.Sendable != 1 || !reflect.DeepEqual(count.Out, map[string]int{"blocked": 1, "missing_variable": 1}) {
		t.Fatalf("count = %+v", count)
	}
	if count.Selection == nil || *count.Selection != (campaignaudience.SelectionCount{Selected: 4, ConfirmCount: 4, Fingerprint: selection.Fingerprint(crmfilter.Filter{})}) {
		t.Fatalf("selection = %+v", count.Selection)
	}
	if !count.Variables[0].Suggested || count.Variables[0].Source != campaign.BindFirstName || count.List != nil {
		t.Fatalf("variables = %+v list = %+v", count.Variables, count.List)
	}
	if w.gate.acquired != 1 || !reflect.DeepEqual(w.channel.opened, []campaignaudience.Source{campaignaudience.SourceLeads}) {
		t.Fatalf("gate = %d opened = %v", w.gate.acquired, w.channel.opened)
	}
}

func TestCountRefusesAnInvalidSpecOrChannelBeforeReadingAnything(t *testing.T) {
	w := newWorld(t)
	if _, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, campaignaudience.Spec{Source: "x"}); !errors.Is(err, campaignaudience.ErrSourceInvalid) {
		t.Fatalf("source = %v", err)
	}
	if _, err := w.service.Count(ctx, testActor, campaignaudience.ChannelCarrier, Params{}, campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: everyoneSelection()}); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("an unwired channel fails closed: %v", err)
	}
	if _, err := w.service.Count(ctx, testActor, campaignaudience.Channel("fax"), Params{}, campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: everyoneSelection()}); !errors.Is(err, campaignaudience.ErrChannelInvalid) {
		t.Fatalf("channel = %v", err)
	}
	w.channel.slots = []campaignaudience.Slot{{Key: "1"}}
	spec := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: everyoneSelection(), Variables: []campaignaudience.Variable{{Key: "9"}}}
	if _, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec); !errors.Is(err, campaignaudience.ErrVariableUnknown) {
		t.Fatalf("unknown variable = %v", err)
	}
	if w.selections.resolved != 0 {
		t.Fatal("a refused count resolved the selection")
	}
}

func TestCountOfAListMatchesRowsToLeadsWithoutCreatingAny(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName}, {Key: "2"}}
	w.channel.judge = fakeJudge{refused: map[string]string{}, fills: map[string][]string{"5511900010142": {"Maria", ""}}}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome;primeiro_nome;var2\n(11) 90001-0142;Maria Souza;;Centro\n5511900023301;João;Jo;\n", View{})
	if preview.Rows != 2 || preview.Existing != 1 || preview.New != 1 || preview.Lines != 2 || !preview.Header {
		t.Fatalf("preview = %+v", preview)
	}
	count, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID})
	if err != nil {
		t.Fatal(err)
	}
	if count.List == nil || *count.List != (campaignaudience.ListCount{ID: preview.ID, Rows: 2, Existing: 1, New: 1}) {
		t.Fatalf("list = %+v", count.List)
	}
	if count.Variables[0].Missing != 0 || count.Variables[1].Missing != 1 || !reflect.DeepEqual(count.Blockers, []campaignaudience.Blocker{campaignaudience.BlockerListVariablesMissing}) {
		t.Fatalf("the first name of Maria comes from her lead, João has no var2: %+v %v", count.Variables, count.Blockers)
	}
	if count.Sendable == nil || *count.Sendable != 2 || count.Selection != nil {
		t.Fatalf("count = %+v", count)
	}
}

func TestAListIsOnlyReadByItsUploaderOnItsChannelBeforeItExpires(t *testing.T) {
	w := newWorld(t)
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone\n5511900010142\n", View{})
	spec := campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}
	other := Actor{WorkspaceID: "ws-1", UserID: "user-2"}
	if _, err := w.service.Count(ctx, other, campaignaudience.ChannelWhatsApp, Params{}, spec); !errors.Is(err, campaignaudience.ErrListNotFound) {
		t.Fatalf("another member = %v", err)
	}
	if _, err := w.service.Count(ctx, Actor{WorkspaceID: "ws-2", UserID: "user-1"}, campaignaudience.ChannelWhatsApp, Params{}, spec); !errors.Is(err, campaignaudience.ErrListNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
	if _, err := w.service.Count(ctx, testActor, campaignaudience.ChannelEmail, Params{}, spec); !errors.Is(err, campaignaudience.ErrListChannelMismatch) {
		t.Fatalf("another channel = %v", err)
	}
	w.service.deps.Now = func() time.Time { return testNow.Add(campaignaudience.ListTTL) }
	if _, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec); !errors.Is(err, campaignaudience.ErrListExpired) {
		t.Fatalf("expired = %v", err)
	}
}

func TestRecipientsOfLeadsConfirmTheCountAndRefuseAnUndecidedVariable(t *testing.T) {
	w := newWorld(t)
	w.selections.ids = []string{"a", "b", "c"}
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName}}
	w.channel.judge = fakeJudge{leads: map[string]campaignaudience.Candidate{
		"a": {HasContact: true, Values: []string{"Ana"}},
		"b": {HasContact: true, Refusal: string(campaign.SkipAlreadyInRunningCampaign)},
		"c": {HasContact: true, Values: []string{""}},
	}}
	stale := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: confirmed(everyoneSelection(), 2), Variables: []campaignaudience.Variable{{Key: "1", Missing: campaignaudience.MissingExclude}}}
	var changed *selection.CountChangedError
	if _, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, stale); !errors.As(err, &changed) || changed.Matched != 3 {
		t.Fatalf("a stale count = %v", err)
	}
	undecided := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: confirmed(everyoneSelection(), 3)}
	if _, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, undecided); !errors.Is(err, campaignaudience.ErrChoiceRequired) {
		t.Fatalf("undecided = %v", err)
	}
	unconfirmed := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: everyoneSelection()}
	if _, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, unconfirmed); !errors.Is(err, selection.ErrEveryoneUnconfirmed) {
		t.Fatalf("unconfirmed = %v", err)
	}
	decided := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: confirmed(everyoneSelection(), 3), Variables: []campaignaudience.Variable{{Key: "1", Missing: campaignaudience.MissingExclude}}}
	got, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, decided)
	if err != nil {
		t.Fatal(err)
	}
	want := []Recipient{
		{LeadID: "a", Variables: []string{"Ana"}},
		{LeadID: "b", Skip: campaign.SkipAlreadyInRunningCampaign},
		{LeadID: "c", Skip: campaign.SkipMissingVariable, Missing: []campaign.MissingVariable{{Slot: 1, Source: campaign.BindFirstName}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recipients = %+v\nwant %+v", got, want)
	}
}

func TestRecipientsOfAListAreTheRowsTheCreateTurnsIntoEntries(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1"}}
	w.channel.judge = fakeJudge{refused: map[string]string{"lead-maria": "opted_out"}}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome;var1;Série\n5511900010142;Maria;M;1\n5511900023301;João;J;\n", View{})
	got, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := []Recipient{
		{Number: "5511900010142", Name: "Maria", Variables: []string{"M"}, Metadata: map[string]any{"serie": float64(1)}},
		{Number: "5511900023301", Name: "João", Variables: []string{"J"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recipients = %+v\nwant %+v", got, want)
	}
	gap := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;var1\n5511900010142;\n", View{})
	if _, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: gap.ID}); !errors.Is(err, campaignaudience.ErrListVariablesMissing) {
		t.Fatalf("a row without a variable = %v", err)
	}
}

func TestUploadRefusesWhatItCannotStore(t *testing.T) {
	w := newWorld(t)
	cases := []struct {
		name    string
		channel campaignaudience.Channel
		up      Upload
		want    error
	}{
		{name: "nothing", channel: campaignaudience.ChannelWhatsApp, up: Upload{FileName: "x.csv"}, want: campaignaudience.ErrFileRequired},
		{name: "too large", channel: campaignaudience.ChannelWhatsApp, up: Upload{Data: make([]byte, campaignaudience.MaxFileBytes+1)}, want: campaignaudience.ErrFileTooLarge},
		{name: "unreadable", channel: campaignaudience.ChannelWhatsApp, up: Upload{Data: []byte("PK\x03\x04garbage")}, want: campaignaudience.ErrFileUnreadable},
		{name: "no phone column", channel: campaignaudience.ChannelWhatsApp, up: Upload{Data: []byte("nome\nMaria\n")}, want: campaign.ErrListContactColumn},
		{name: "carrier", channel: campaignaudience.ChannelCarrier, up: Upload{Data: []byte("telefone\n5511900010142\n")}, want: campaignaudience.ErrSourceUnsupported},
		{name: "unknown channel", channel: "fax", up: Upload{Data: []byte("telefone\n5511900010142\n")}, want: campaignaudience.ErrChannelInvalid},
	}
	for _, tc := range cases {
		if _, err := w.service.Upload(ctx, testActor, tc.channel, tc.up, View{}); !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if w.lists.Len() != 0 {
		t.Fatal("a refused upload stored a list")
	}
}

func TestUploadOfAnEmptyListAcceptsRowsTypedByHand(t *testing.T) {
	w := newWorld(t)
	preview, err := w.service.Upload(ctx, testActor, campaignaudience.ChannelWhatsApp, Upload{Empty: true}, View{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rows != 0 || preview.Lines != 0 || preview.ExpiresAt == nil {
		t.Fatalf("preview = %+v", preview)
	}
	row, err := w.service.AddRow(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowInput{Contact: "(11) 90001-0142", Name: "Maria", Variables: []string{"M"}})
	if err != nil {
		t.Fatal(err)
	}
	if row.Contact != "5511900010142" || row.Line != 0 || !reflect.DeepEqual(row.Variables, []string{"M"}) {
		t.Fatalf("row = %+v", row)
	}
	if _, err := w.service.AddRow(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowInput{Contact: "5511900010142"}); !errors.Is(err, campaignaudience.ErrRowDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	if _, err := w.service.AddRow(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowInput{Contact: "123"}); !errors.Is(err, campaignaudience.ErrRowContactInvalid) {
		t.Fatalf("invalid = %v", err)
	}
	edited, err := w.service.EditRow(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, row.ID, RowInput{Contact: "5511900023301", Name: "João", Variables: []string{"J"}, Metadata: map[string]any{"serie": "2"}})
	if err != nil || edited.Contact != "5511900023301" || edited.Name != "João" || edited.Metadata["serie"] != "2" {
		t.Fatalf("edited = %+v, %v", edited, err)
	}
	if _, err := w.service.EditRow(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, "missing", RowInput{Contact: "5511900023302"}); !errors.Is(err, campaignaudience.ErrRowNotFound) {
		t.Fatalf("missing row = %v", err)
	}
	if err := w.service.RemoveRow(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, row.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := w.lists.CountRows(ctx, preview.ID); n != 0 {
		t.Fatalf("rows after removal = %d", n)
	}
	if _, err := w.service.AddRow(ctx, Actor{WorkspaceID: "ws-1", UserID: "user-2"}, campaignaudience.ChannelWhatsApp, preview.ID, RowInput{Contact: "5511900010199"}); !errors.Is(err, campaignaudience.ErrListNotFound) {
		t.Fatalf("another member = %v", err)
	}
}

func TestAChangeRenewsTheListTTL(t *testing.T) {
	w := newWorld(t)
	preview, err := w.service.Upload(ctx, testActor, campaignaudience.ChannelWhatsApp, Upload{Empty: true}, View{})
	if err != nil {
		t.Fatal(err)
	}
	later := testNow.Add(20 * time.Hour)
	w.service.deps.Now = func() time.Time { return later }
	if _, err := w.service.AddRow(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowInput{Contact: "5511900010142"}); err != nil {
		t.Fatal(err)
	}
	stored, _ := w.lists.Get(ctx, "ws-1", preview.ID)
	if stored.ExpiresAt == nil || !stored.ExpiresAt.Equal(later.Add(campaignaudience.ListTTL)) {
		t.Fatalf("expires at %v", stored.ExpiresAt)
	}
}

func TestRowsListsAPageAndCanKeepOnlyTheRowsMissingAVariable(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1"}, {Key: "2"}}
	w.channel.judge = fakeJudge{}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome;var1;var2\n5511900010142;Maria;M;C\n5511900023301;João;J;\n5511900033301;Rita;;\n", View{})
	page, err := w.service.Rows(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowsQuery{Page: 1, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].Name != "Maria" || !reflect.DeepEqual(page.Items[1].Missing, []int{2}) {
		t.Fatalf("page = %+v", page)
	}
	missing, err := w.service.Rows(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowsQuery{MissingOnly: true, Page: 1, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Total != 2 || missing.Items[0].Name != "João" || !reflect.DeepEqual(missing.Items[1].Missing, []int{1, 2}) {
		t.Fatalf("missing = %+v", missing)
	}
	search, err := w.service.Rows(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowsQuery{Search: "rita", Page: 1, Limit: 50})
	if err != nil || search.Total != 1 || search.Items[0].Contact != "5511900033301" {
		t.Fatalf("search = %+v, %v", search, err)
	}
	if _, err := w.service.Rows(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowsQuery{Page: 1, Limit: campaignaudience.MaxRowsPage + 1}); !errors.Is(err, ErrPageInvalid) {
		t.Fatalf("page size = %v", err)
	}
}

func TestPreviewSaysWhichColumnFillsEachVariable(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName}, {Key: "2", Suggested: campaign.CustomBinding("serie")}, {Key: "3"}}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome;Série;var3;Turma\n5511900010142;Maria;1;x;A\n", View{Params: Params{TemplateID: "tpl"}})
	want := []campaign.VariableColumnMapping{
		{Slot: 1, Field: campaign.BindFirstName, Fill: campaign.FillName},
		{Slot: 2, Column: "Série", Field: campaign.CustomBinding("serie"), Fill: campaign.FillColumn},
		{Slot: 3, Column: "var3", Fill: campaign.FillColumn},
	}
	if !reflect.DeepEqual(preview.Columns.Variables, want) {
		t.Fatalf("mapping = %+v\nwant %+v", preview.Columns.Variables, want)
	}
	if !reflect.DeepEqual(preview.Columns.Metadata, []campaign.MetadataColumn{{Key: "turma", Label: "Turma"}}) || preview.Columns.Contact != "telefone" || preview.Columns.Name != "nome" {
		t.Fatalf("columns = %+v", preview.Columns)
	}
	plain := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;var1;var2\n5511900010142;a;b\n", View{})
	if len(plain.Columns.Variables) != 2 || plain.Columns.Variables[1].Column != "var2" || plain.VariableColumns != 2 {
		t.Fatalf("without a template the var columns are listed: %+v", plain.Columns)
	}
}

func TestPreviewListsTheSkippedLinesWithTheirReason(t *testing.T) {
	w := newWorld(t)
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome\n5511900010142;Maria\n123;Bruna\n5511900010142;Maria\n", View{})
	if preview.Skipped.Total != 2 || preview.Skipped.ByReason[campaign.ListInvalidNumber] != 1 || preview.Skipped.ByReason[campaign.ListDuplicate] != 1 {
		t.Fatalf("skipped = %+v", preview.Skipped)
	}
	if preview.Skipped.Lines[1] != (campaign.ListIssue{Line: 4, Reason: campaign.ListDuplicate, Raw: "5511900010142;Maria", DuplicateOf: 2}) {
		t.Fatalf("lines = %+v", preview.Skipped.Lines)
	}
	again, err := w.service.Preview(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, View{})
	if err != nil || again.ID != preview.ID || again.Skipped.Total != 2 {
		t.Fatalf("preview again = %+v, %v", again, err)
	}
}

func TestTheSampleOfATemplateNamesSuggestedColumnsAndReadsBackClean(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName, Example: "Maria"}, {Key: "2", Example: "Centro"}, {Key: "3", Suggested: campaign.CustomBinding("serie")}}
	file, err := w.service.Sample(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, nil, SampleCSV)
	if err != nil {
		t.Fatal(err)
	}
	text := string(file.Data)
	if !strings.HasPrefix(text, "number;name;primeiro_nome;var2;serie\n") || file.Name != "modelo-publico.csv" || !strings.HasPrefix(file.ContentType, "text/csv") {
		t.Fatalf("sample %s = %q", file.Name, text)
	}
	w.channel.judge = fakeJudge{}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, text, View{Params: Params{TemplateID: "tpl"}})
	if preview.Skipped.Total != 0 || preview.Rows != 1 {
		t.Fatalf("the sample reads back with skipped lines: %+v", preview)
	}
	if got := preview.Columns.Variables; len(got) != 3 || got[0].Column != "primeiro_nome" || got[2].Column != "serie" || got[1].Column != "var2" {
		t.Fatalf("mapping = %+v", got)
	}
	email, err := w.service.Sample(ctx, testActor, campaignaudience.ChannelEmail, Params{}, nil, SampleCSV)
	if err != nil || !strings.HasPrefix(string(email.Data), "email;name\n") {
		t.Fatalf("email sample = %q, %v", email.Data, err)
	}
	if _, err := w.service.Sample(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, nil, "pdf"); !errors.Is(err, ErrSampleFormat) {
		t.Fatalf("format = %v", err)
	}
}

func TestFieldsAreTheBindableSourcesAndTheVisibleCustomFields(t *testing.T) {
	w := newWorld(t)
	fields, err := w.service.Fields(ctx, testActor)
	if err != nil {
		t.Fatal(err)
	}
	sources := []campaign.BindingSource{}
	for _, f := range fields {
		sources = append(sources, f.Source)
	}
	want := append(campaign.BindableSources(), campaign.CustomBinding("serie"))
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("fields = %v", sources)
	}
	last := fields[len(fields)-1]
	if last.Kind != FieldCustom || last.Key != "serie" || last.Label != "Série" || last.Column != "serie" || fields[0].Kind != FieldLead || fields[0].Column != "primeiro_nome" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestAnEmailListAttachedToADraftTurnsItsRowsIntoLeads(t *testing.T) {
	w := newWorld(t)
	preview := w.upload(t, campaignaudience.ChannelEmail, "email;nome\nana@exemplo.com.br;Ana\nbia@exemplo.com.br;Bia\n", View{})
	if preview.Existing != 1 || preview.New != 1 || preview.Contact != campaign.ContactEmail {
		t.Fatalf("preview = %+v", preview)
	}
	owner := campaignaudience.EmailCampaignOwner("camp-1")
	if err := w.service.Attach(ctx, testActor, preview.ID, owner); err != nil {
		t.Fatal(err)
	}
	if err := w.service.EnsureLeads(ctx, "ws-1", owner, preview.ID); err != nil {
		t.Fatal(err)
	}
	if len(w.emailLeads.created) != 2 || w.emailLeads.created[1].Email != "bia@exemplo.com.br" || w.emailLeads.created[1].Name != "Bia" {
		t.Fatalf("leads = %+v", w.emailLeads.created)
	}
	w.service.deps.Now = func() time.Time { return testNow.Add(30 * 24 * time.Hour) }
	if _, err := w.service.Count(ctx, Actor{WorkspaceID: "ws-1", UserID: "user-9"}, campaignaudience.ChannelEmail, Params{}, campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}); err != nil {
		t.Fatalf("an attached list is read by any member editing the draft, and never expires: %v", err)
	}
	if err := w.service.Release(ctx, "ws-1", owner, preview.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.lists.Get(ctx, "ws-1", preview.ID); !errors.Is(err, campaignaudience.ErrListNotFound) {
		t.Fatalf("a released list is deleted: %v", err)
	}
	if err := w.service.EnsureLeads(ctx, "ws-1", owner, preview.ID); !errors.Is(err, campaignaudience.ErrListNotFound) {
		t.Fatalf("a launch without its list = %v", err)
	}
}

func TestSweepDeletesOnlyExpiredLists(t *testing.T) {
	w := newWorld(t)
	old := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone\n5511900010142\n", View{})
	w.service.deps.Now = func() time.Time { return testNow.Add(campaignaudience.ListTTL + time.Minute) }
	fresh := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone\n5511900010142\n", View{})
	n, err := w.service.Sweep(ctx)
	if err != nil || n != 1 {
		t.Fatalf("Sweep() = %d, %v", n, err)
	}
	if _, err := w.lists.Get(ctx, "ws-1", old.ID); !errors.Is(err, campaignaudience.ErrListNotFound) {
		t.Fatal("the expired list stayed")
	}
	if _, err := w.lists.Get(ctx, "ws-1", fresh.ID); err != nil {
		t.Fatal("the fresh list was deleted")
	}
}

func TestNewFailsClosedWithoutItsDependencies(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("New(Deps{}) = %v", err)
	}
}
