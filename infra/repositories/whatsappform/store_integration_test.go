package whatsappform_repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

type world struct {
	t            *testing.T
	db           *gorm.DB
	forms        *FormStore
	versions     *VersionStore
	publications *PublicationStore
	sends        *SendStore
	responses    *ResponseStore
	ws           string
	phone        string
	at           time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := repotest.IsolatedDB(t, "whatsapp_form")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := database.CreateSearchFold(db); err != nil {
		t.Fatalf("search fold: %v", err)
	}
	if err := db.AutoMigrate(
		&schema.WhatsAppForm{}, &schema.WhatsAppFormVersion{}, &schema.WhatsAppFormPublication{},
		&schema.WhatsAppFormSend{}, &schema.WhatsAppFormResponse{},
		&schema.Lead{}, &schema.WhatsAppCampaign{}, &schema.WhatsAppCampaignEntry{},
		&schema.InboxAssignment{}, &schema.WhatsAppMessageStatusEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &world{
		t: t, db: db, forms: NewFormStore(db), versions: NewVersionStore(db), publications: NewPublicationStore(db),
		sends: NewSendStore(db), responses: NewResponseStore(db),
		ws: uuid.NewString(), phone: uuid.NewString(), at: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
}

func (w *world) exec(sql string, args ...interface{}) {
	w.t.Helper()
	if err := w.db.Exec(sql, args...).Error; err != nil {
		w.t.Fatalf("exec %s: %v", sql, err)
	}
}

func (w *world) form(workspaceID, name string) (whatsappform.Form, whatsappform.Version) {
	w.t.Helper()
	form, version := w.newForm(workspaceID, name)
	if err := w.forms.Create(ctx, form, version); err != nil {
		w.t.Fatalf("create form %q: %v", name, err)
	}
	return form, version
}

func (w *world) newForm(workspaceID, name string) (whatsappform.Form, whatsappform.Version) {
	form := whatsappform.Form{ID: uuid.NewString(), WorkspaceID: workspaceID, Name: name, Kind: whatsappform.KindBuilt, CreatedAt: w.at, UpdatedAt: w.at}
	version := w.version(form, 1)
	form.DraftVersionID = version.ID
	return form, version
}

func (w *world) version(form whatsappform.Form, number int) whatsappform.Version {
	return whatsappform.Version{
		ID: uuid.NewString(), WorkspaceID: form.WorkspaceID, FormID: form.ID, Number: number, CreatedAt: w.at,
		Document: whatsappform.Document{Schema: 1, Screens: []whatsappform.Screen{{Key: "s_one", Title: "Início"}}},
		FlowJSON: []byte(`{"version": "7.3", "screens": []}`),
		Hash:     "h", CompilerVersion: "c1", FlowVersion: "7.3",
	}
}

func (w *world) entry(departmentID string) string {
	w.t.Helper()
	campaignID, entryID := uuid.NewString(), uuid.NewString()
	w.exec("INSERT INTO whatsapp_campaigns (id, workspace_id, department_id, name) VALUES (?, ?, ?, ?)", campaignID, w.ws, schema.OptionalText(departmentID), "c")
	w.exec("INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id) VALUES (?, ?, ?)", entryID, campaignID, uuid.NewString())
	return entryID
}

func (w *world) campaignOf(entryID string) string {
	w.t.Helper()
	var campaign string
	if err := w.db.Raw("SELECT campaign_id::text FROM whatsapp_campaign_entries WHERE id = ?", entryID).Scan(&campaign).Error; err != nil {
		w.t.Fatal(err)
	}
	return campaign
}

func (w *world) send(form whatsappform.Form, entryID string, status whatsappform.SendStatus, created time.Time, messageID string, origin whatsappform.SendOrigin) whatsappform.Send {
	w.t.Helper()
	s := whatsappform.Send{
		ID: uuid.NewString(), WorkspaceID: w.ws, FormID: form.ID, VersionID: form.DraftVersionID, PublicationID: uuid.NewString(),
		Token: "vf1_" + uuid.NewString(), Origin: origin, EntryID: entryID, EntryType: string(shared.EntryTypeWhatsApp),
		BusinessPhoneID: w.phone, ContactNumber: "5511999990000", Status: status, MessageID: messageID, CreatedAt: created,
	}
	if status == whatsappform.SendSent || status == whatsappform.SendResponded {
		s.SentAt = &created
	}
	if err := w.sends.Insert(ctx, s); err != nil {
		w.t.Fatalf("insert send: %v", err)
	}
	return s
}

func (w *world) response(form whatsappform.Form, entryID, leadID string, match whatsappform.MatchResult, submitted time.Time, sendID string, options ...string) whatsappform.Response {
	w.t.Helper()
	r := whatsappform.Response{
		ID: uuid.NewString(), WorkspaceID: w.ws, FormID: form.ID, VersionID: form.DraftVersionID, SendID: sendID, Origin: whatsappform.OriginInbox,
		Match: match, InboundMessageID: "wamid." + uuid.NewString(), EntryID: entryID, EntryType: string(shared.EntryTypeWhatsApp), LeadID: leadID,
		BusinessPhoneID: w.phone, ContactNumber: "5511999990000", ApplyStatus: whatsappform.ApplyPending, SubmittedAt: submitted, CreatedAt: submitted,
		Answers: whatsappform.Answers{Values: map[string]whatsappform.Answer{"f_color": {Key: "f_color", Kind: whatsappform.FieldCheckbox, OptionIDs: options}}},
		Raw:     []byte(`{"f_color": []}`),
	}
	stored, created, err := w.responses.Insert(ctx, r)
	if err != nil || !created {
		w.t.Fatalf("insert response: %v, created %v", err, created)
	}
	return stored
}

func TestIntegrationFormNamesAreUniqueAmongLiveFormsOnly(t *testing.T) {
	w := newWorld(t)
	first, _ := w.form(w.ws, "Pesquisa de Satisfação")
	twin, twinVersion := w.newForm(w.ws, "pesquisa de satisfação")
	if err := w.forms.Create(ctx, twin, twinVersion); !errors.Is(err, whatsappform.ErrNameTaken) {
		t.Fatalf("same name in the workspace: %v", err)
	}
	if _, err := w.versions.Get(ctx, w.ws, twinVersion.ID); !errors.Is(err, whatsappform.ErrVersionNotFound) {
		t.Fatalf("the refused form left its version behind: %v", err)
	}
	w.form(uuid.NewString(), "Pesquisa de Satisfação")
	other, _ := w.form(w.ws, "Outra")
	if err := w.forms.Rename(ctx, w.ws, other.ID, "PESQUISA DE SATISFAÇÃO", "", w.at); !errors.Is(err, whatsappform.ErrNameTaken) {
		t.Fatalf("rename onto a live name: %v", err)
	}
	if err := w.forms.Archive(ctx, w.ws, first.ID, w.at); err != nil {
		t.Fatal(err)
	}
	if err := w.forms.Create(ctx, twin, twinVersion); err != nil {
		t.Fatalf("an archived name is free again: %v", err)
	}
	active, err := w.forms.List(ctx, whatsappform.FormQuery{WorkspaceID: w.ws})
	if err != nil || len(active) != 2 {
		t.Fatalf("active forms = %+v, %v", active, err)
	}
	all, err := w.forms.List(ctx, whatsappform.FormQuery{WorkspaceID: w.ws, IncludeArchived: true})
	if err != nil || len(all) != 3 {
		t.Fatalf("all forms = %+v, %v", all, err)
	}
	found, err := w.forms.List(ctx, whatsappform.FormQuery{WorkspaceID: w.ws, Search: "satisfacao"})
	if err != nil || len(found) != 1 || found[0].Form.ID != twin.ID {
		t.Fatalf("accent-insensitive search = %+v, %v", found, err)
	}
	if _, err := w.forms.Get(ctx, uuid.NewString(), first.ID); !errors.Is(err, whatsappform.ErrFormNotFound) {
		t.Fatalf("form of another workspace: %v", err)
	}
}

func TestIntegrationDraftSavesAreCompareAndSwap(t *testing.T) {
	w := newWorld(t)
	form, first := w.form(w.ws, "F")
	second := w.version(form, 2)
	if err := w.forms.SaveDraft(ctx, w.ws, form.ID, first.ID, second, w.at); err != nil {
		t.Fatal(err)
	}
	if err := w.forms.SaveDraft(ctx, w.ws, form.ID, first.ID, w.version(form, 3), w.at); !errors.Is(err, whatsappform.ErrStaleDraft) {
		t.Fatalf("save from an old base: %v", err)
	}
	if err := w.forms.SaveDraft(ctx, w.ws, form.ID, second.ID, w.version(form, 2), w.at); !errors.Is(err, whatsappform.ErrStaleDraft) {
		t.Fatalf("save onto a taken number: %v", err)
	}
	got, err := w.forms.Get(ctx, w.ws, form.ID)
	if err != nil || got.DraftVersionID != second.ID {
		t.Fatalf("draft = %+v, %v", got, err)
	}
	versions, err := w.versions.List(ctx, w.ws, form.ID)
	if err != nil || len(versions) != 2 || versions[0].Number != 2 || versions[1].Number != 1 {
		t.Fatalf("versions = %+v, %v", versions, err)
	}
	if string(versions[0].FlowJSON) != `{"version": "7.3", "screens": []}` || versions[0].Document.Screens[0].Title != "Início" {
		t.Fatalf("version content = %+v", versions[0])
	}
	if err := w.forms.SetPublished(ctx, w.ws, form.ID, second.ID, w.at); err != nil {
		t.Fatal(err)
	}
	other, otherVersion := w.form(w.ws, "G")
	if err := w.forms.SetPublished(ctx, w.ws, form.ID, otherVersion.ID, w.at); !errors.Is(err, whatsappform.ErrFormNotFound) {
		t.Fatalf("publishing a version of %s: %v", other.ID, err)
	}
}

func TestIntegrationConcurrentDraftSavesFromTheSameBaseLetOneWin(t *testing.T) {
	w := newWorld(t)
	form, first := w.form(w.ws, "F")
	const savers = 4
	errs := make([]error, savers)
	saved := make([]whatsappform.Version, savers)
	var wg sync.WaitGroup
	for i := range savers {
		saved[i] = w.version(form, 2)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = w.forms.SaveDraft(ctx, w.ws, form.ID, first.ID, saved[i], w.at)
		}(i)
	}
	wg.Wait()
	winner := ""
	for i, err := range errs {
		switch {
		case err == nil && winner == "":
			winner = saved[i].ID
		case err == nil:
			t.Fatalf("two saves won: %s and %s", winner, saved[i].ID)
		case !errors.Is(err, whatsappform.ErrStaleDraft):
			t.Fatalf("a losing save failed with %v", err)
		}
	}
	got, err := w.forms.Get(ctx, w.ws, form.ID)
	if err != nil || winner == "" || got.DraftVersionID != winner {
		t.Fatalf("draft = %+v, winner %q, %v", got, winner, err)
	}
	versions, err := w.versions.List(ctx, w.ws, form.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %+v, %v", versions, err)
	}
}

func TestIntegrationResponseInsertIsIdempotent(t *testing.T) {
	w := newWorld(t)
	form, _ := w.form(w.ws, "F")
	stored := w.response(form, "", "", whatsappform.MatchByToken, w.at, "", "o_red")
	again := stored
	again.ID = uuid.NewString()
	got, created, err := w.responses.Insert(ctx, again)
	if err != nil || created || got.ID != stored.ID || got.Answers.Values["f_color"].OptionIDs[0] != "o_red" {
		t.Fatalf("redelivery = %+v, %v, %v", got, created, err)
	}
	report := whatsappform.ApplyReport{Changed: []string{"name"}}
	if err := w.responses.SaveApply(ctx, w.ws, stored.ID, whatsappform.ApplyApplied, report); err != nil {
		t.Fatal(err)
	}
	read, err := w.responses.Get(ctx, w.ws, stored.ID, whatsappform.ReadScope{Resolved: true})
	if err != nil || read.ApplyStatus != whatsappform.ApplyApplied || len(read.Report.Changed) != 1 {
		t.Fatalf("after apply = %+v, %v", read, err)
	}
	if err := w.responses.Delete(ctx, w.ws, stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.responses.Get(ctx, w.ws, stored.ID, whatsappform.ReadScope{Resolved: true}); !errors.Is(err, whatsappform.ErrResponseNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestIntegrationResponsesPageByKeysetWithoutGapsOrRepeats(t *testing.T) {
	w := newWorld(t)
	form, _ := w.form(w.ws, "F")
	want := map[string]bool{}
	for i := range 5 {
		submitted := w.at.Add(-time.Duration(i/2) * time.Minute)
		want[w.response(form, "", "", whatsappform.MatchByToken, submitted, "").ID] = true
	}
	seen, after, pages := map[string]bool{}, "", 0
	for {
		page, err := w.responses.List(ctx, whatsappform.ResponseQuery{WorkspaceID: w.ws, FormID: form.ID, Limit: 2, After: after, Scope: whatsappform.ReadScope{Resolved: true}})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, r := range page.Items {
			if seen[r.ID] {
				t.Fatalf("response %s came twice", r.ID)
			}
			seen[r.ID] = true
		}
		if page.Next == "" {
			break
		}
		after = page.Next
	}
	if pages != 3 || len(seen) != len(want) {
		t.Fatalf("pages = %d, seen %d of %d", pages, len(seen), len(want))
	}
}

func TestIntegrationResponseSearchFindsTheLeadNameWithoutAccents(t *testing.T) {
	w := newWorld(t)
	form, _ := w.form(w.ws, "F")
	leadID := uuid.NewString()
	if err := w.db.Create(&schema.Lead{ID: leadID, WorkspaceID: w.ws, Name: "José Araújo", Number: "5511988887777"}).Error; err != nil {
		t.Fatal(err)
	}
	named := w.response(form, "", leadID, whatsappform.MatchByToken, w.at, "")
	w.response(form, "", "", whatsappform.UnmatchedNoToken, w.at, "")
	cases := []struct {
		name  string
		query whatsappform.ResponseQuery
		want  int
	}{
		{"by lead name", whatsappform.ResponseQuery{Search: "jose araujo"}, 1},
		{"by number", whatsappform.ResponseQuery{Search: "99999"}, 2},
		{"matched only", whatsappform.ResponseQuery{Match: whatsappform.MatchFilterMatched}, 1},
		{"unmatched only", whatsappform.ResponseQuery{Match: whatsappform.MatchFilterUnmatched}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.query.WorkspaceID, tc.query.FormID, tc.query.Scope = w.ws, form.ID, whatsappform.ReadScope{Resolved: true}
			page, err := w.responses.List(ctx, tc.query)
			if err != nil || len(page.Items) != tc.want {
				t.Fatalf("got %d, %v, want %d", len(page.Items), err, tc.want)
			}
			if tc.name == "by lead name" && page.Items[0].ID != named.ID {
				t.Fatalf("found %s", page.Items[0].ID)
			}
		})
	}
}

func TestIntegrationRestrictedReadersSeeTheirDepartmentOrWhatIsAssignedToThem(t *testing.T) {
	w := newWorld(t)
	form, _ := w.form(w.ws, "F")
	sales, support := uuid.NewString(), uuid.NewString()
	user := uuid.NewString()
	salesEntry, supportEntry, assignedEntry := w.entry(sales), w.entry(support), w.entry(support)
	w.exec("INSERT INTO inbox_assignments (id, workspace_id, entry_id, entry_type, assigned_user_id) VALUES (?, ?, ?, ?, ?)",
		uuid.NewString(), w.ws, assignedEntry, string(shared.EntryTypeWhatsApp), user)
	inSales := w.response(form, salesEntry, "", whatsappform.MatchByToken, w.at, "")
	inSupport := w.response(form, supportEntry, "", whatsappform.MatchByToken, w.at.Add(-time.Minute), "")
	assigned := w.response(form, assignedEntry, "", whatsappform.MatchByToken, w.at.Add(-2*time.Minute), "")
	loose := w.response(form, "", "", whatsappform.UnmatchedNoToken, w.at.Add(-3*time.Minute), "")
	w.send(form, salesEntry, whatsappform.SendSent, w.at, "wamid.a", whatsappform.OriginInbox)
	w.send(form, supportEntry, whatsappform.SendSent, w.at, "wamid.b", whatsappform.OriginInbox)
	w.send(form, assignedEntry, whatsappform.SendSent, w.at, "wamid.c", whatsappform.OriginInbox)

	cases := []struct {
		name  string
		scope whatsappform.ReadScope
		want  []string
		sends int
	}{
		{"unrestricted", whatsappform.ReadScope{Resolved: true}, []string{inSales.ID, inSupport.ID, assigned.ID, loose.ID}, 3},
		{"sales with assignments", whatsappform.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{sales}, AssigneeUserID: user}, []string{inSales.ID, assigned.ID}, 2},
		{"sales only", whatsappform.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{sales}}, []string{inSales.ID}, 1},
		{"assignments only", whatsappform.ReadScope{Resolved: true, Restrict: true, AssigneeUserID: user}, []string{assigned.ID}, 1},
		{"no department", whatsappform.ReadScope{Resolved: true, Restrict: true}, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := w.responses.List(ctx, whatsappform.ResponseQuery{WorkspaceID: w.ws, FormID: form.ID, Scope: tc.scope})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != len(tc.want) {
				t.Fatalf("got %d responses, want %d", len(page.Items), len(tc.want))
			}
			for i, r := range page.Items {
				if r.ID != tc.want[i] {
					t.Fatalf("item %d = %s, want %s", i, r.ID, tc.want[i])
				}
			}
			for _, id := range []string{inSales.ID, inSupport.ID, assigned.ID, loose.ID} {
				_, err := w.responses.Get(ctx, w.ws, id, tc.scope)
				visible := false
				for _, wantID := range tc.want {
					visible = visible || wantID == id
				}
				if visible != (err == nil) || (!visible && !errors.Is(err, whatsappform.ErrResponseNotFound)) {
					t.Fatalf("get %s visible=%v err=%v", id, visible, err)
				}
			}
			sends, err := w.sends.List(ctx, whatsappform.SendQuery{WorkspaceID: w.ws, FormID: form.ID, Scope: tc.scope})
			if err != nil || len(sends.Items) != tc.sends {
				t.Fatalf("sends = %d, %v, want %d", len(sends.Items), err, tc.sends)
			}
		})
	}
	campaign, err := w.responses.List(ctx, whatsappform.ResponseQuery{WorkspaceID: w.ws, FormID: form.ID, CampaignID: w.campaignOf(salesEntry), Scope: whatsappform.ReadScope{Resolved: true}})
	if err != nil || len(campaign.Items) != 1 || campaign.Items[0].ID != inSales.ID || campaign.Items[0].CampaignID != w.campaignOf(salesEntry) {
		t.Fatalf("campaign filter = %+v, %v", campaign, err)
	}
}

func TestIntegrationStatsAndDistribution(t *testing.T) {
	w := newWorld(t)
	form, _ := w.form(w.ws, "F")
	day1, day2 := w.at.Add(-24*time.Hour), w.at
	first := w.send(form, "", whatsappform.SendResponded, day1, "wamid.1", whatsappform.OriginInbox)
	second := w.send(form, "", whatsappform.SendResponded, day2, "wamid.2", whatsappform.OriginCampaign)
	w.send(form, "", whatsappform.SendSent, day2, "wamid.3", whatsappform.OriginCampaign)
	w.send(form, "", whatsappform.SendFailed, day2, "", whatsappform.OriginCampaign)
	for _, event := range []struct{ message, status string }{{"wamid.1", "delivered"}, {"wamid.1", "read"}, {"wamid.2", "delivered"}, {"wamid.9", "read"}} {
		w.exec("INSERT INTO whatsapp_message_status_events (message_id, status, phone_number_id, occurred_at, recorded_at) VALUES (?, ?, ?, ?, ?)",
			event.message, event.status, "pn", w.at, w.at)
	}
	w.response(form, "", "", whatsappform.MatchByToken, day1.Add(60*time.Second), first.ID, "o_red", "o_blue")
	w.response(form, "", "", whatsappform.MatchByContext, day2.Add(120*time.Second), second.ID, "o_red")
	w.response(form, "", "", whatsappform.UnmatchedUnknownToken, day2, "", "o_red")

	stats, err := w.responses.Stats(ctx, whatsappform.StatsQuery{WorkspaceID: w.ws, FormID: form.ID, Scope: whatsappform.ReadScope{Resolved: true}})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sends[whatsappform.SendResponded] != 2 || stats.Sends[whatsappform.SendSent] != 1 || stats.Sends[whatsappform.SendFailed] != 1 ||
		stats.Delivered != 2 || stats.Read != 1 || stats.Responses != 2 || stats.MedianSecondsToRespond == nil || *stats.MedianSecondsToRespond != 90 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(stats.ByDay) != 2 || stats.ByDay[0].Day != "2026-10-18" || stats.ByDay[0].Sent != 1 || stats.ByDay[1].Sent != 2 || stats.ByDay[1].Responses != 1 {
		t.Fatalf("by day = %+v", stats.ByDay)
	}
	if len(stats.ByOrigin) != 2 || stats.ByOrigin[0].Origin != whatsappform.OriginInbox || stats.ByOrigin[1].Sent != 2 {
		t.Fatalf("by origin = %+v", stats.ByOrigin)
	}
	from := w.at.Add(-time.Hour)
	recent, err := w.responses.Stats(ctx, whatsappform.StatsQuery{WorkspaceID: w.ws, FormID: form.ID, From: &from, Scope: whatsappform.ReadScope{Resolved: true}})
	if err != nil || recent.Responses != 1 || recent.Sends[whatsappform.SendResponded] != 1 {
		t.Fatalf("ranged stats = %+v, %v", recent, err)
	}
	counts, err := w.responses.Distribution(ctx, whatsappform.StatsQuery{WorkspaceID: w.ws, FormID: form.ID, Scope: whatsappform.ReadScope{Resolved: true}}, "f_color")
	if err != nil || counts["o_red"] != 2 || counts["o_blue"] != 1 || len(counts) != 2 {
		t.Fatalf("distribution = %v, %v", counts, err)
	}
	none, err := w.responses.Distribution(ctx, whatsappform.StatsQuery{WorkspaceID: w.ws, FormID: form.ID, Scope: whatsappform.ReadScope{Resolved: true}}, "f_missing")
	if err != nil || len(none) != 0 {
		t.Fatalf("missing field = %v, %v", none, err)
	}

	summaries, err := w.forms.List(ctx, whatsappform.FormQuery{WorkspaceID: w.ws})
	if err != nil || len(summaries) != 1 || summaries[0].Sent != 3 || summaries[0].Responses != 3 || summaries[0].LastResponseAt == nil {
		t.Fatalf("summaries = %+v, %v", summaries, err)
	}
}

func TestIntegrationSendsAreFoundByTokenAndMessage(t *testing.T) {
	w := newWorld(t)
	form, _ := w.form(w.ws, "F")
	s := w.send(form, "", whatsappform.SendPrepared, w.at, "", whatsappform.OriginWorkflow)
	if err := s.MarkSent("wamid.x", w.at); err != nil {
		t.Fatal(err)
	}
	if err := w.sends.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	byToken, err := w.sends.FindByToken(ctx, s.Token)
	if err != nil || byToken.Status != whatsappform.SendSent || byToken.MessageID != "wamid.x" {
		t.Fatalf("by token = %+v, %v", byToken, err)
	}
	byMessage, err := w.sends.FindByMessageID(ctx, "wamid.x")
	if err != nil || byMessage.ID != s.ID {
		t.Fatalf("by message = %+v, %v", byMessage, err)
	}
	duplicate := s
	duplicate.ID = uuid.NewString()
	if err := w.sends.Insert(ctx, duplicate); err == nil {
		t.Fatal("a token is used once")
	}
}

func TestIntegrationPublicationsAreOnePerAccountAndFlow(t *testing.T) {
	w := newWorld(t)
	form, version := w.form(w.ws, "F")
	p := whatsappform.Publication{
		ID: uuid.NewString(), WorkspaceID: w.ws, FormID: form.ID, WABAID: uuid.NewString(), MetaWABAID: "waba-1", MetaName: "F · Vozko",
		VersionID: version.ID, Status: whatsappform.PublicationCreating, CreatedAt: w.at.Add(-time.Hour), UpdatedAt: w.at.Add(-time.Hour),
	}
	if err := w.publications.Insert(ctx, p); err != nil {
		t.Fatal(err)
	}
	twin := p
	twin.ID = uuid.NewString()
	if err := w.publications.Insert(ctx, twin); err == nil {
		t.Fatal("one publication per form and account")
	}
	stuck, err := w.publications.ListStuck(ctx, w.at, 10)
	if err != nil || len(stuck) != 1 || stuck[0].ID != p.ID {
		t.Fatalf("stuck = %+v, %v", stuck, err)
	}
	p.MetaFlowID, p.Status, p.UpdatedAt, p.PublishedAt = "flow-1", whatsappform.PublicationPublished, w.at, &w.at
	if err := w.publications.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	found, err := w.publications.FindByMetaFlow(ctx, "waba-1", "flow-1")
	if err != nil || found.ID != p.ID || !found.Sendable() {
		t.Fatalf("by flow = %+v, %v", found, err)
	}
	if stuck, _ := w.publications.ListStuck(ctx, w.at.Add(time.Hour), 10); len(stuck) != 0 {
		t.Fatalf("published is not stuck: %+v", stuck)
	}
	summaries, err := w.forms.List(ctx, whatsappform.FormQuery{WorkspaceID: w.ws})
	if err != nil || len(summaries[0].Publications) != 1 || summaries[0].Publications[0].Status != whatsappform.PublicationPublished {
		t.Fatalf("summary publications = %+v, %v", summaries, err)
	}
	otherForm, otherVersion := w.form(w.ws, "G")
	clash := whatsappform.Publication{
		ID: uuid.NewString(), WorkspaceID: w.ws, FormID: otherForm.ID, MetaWABAID: "waba-1", MetaFlowID: "flow-1", MetaName: "G",
		VersionID: otherVersion.ID, Status: whatsappform.PublicationDraft, CreatedAt: w.at, UpdatedAt: w.at,
	}
	if err := w.publications.Insert(ctx, clash); err == nil {
		t.Fatal("one publication per Meta flow")
	}
}
