package unofficial_whatsapp_group_repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database/schema"
)

func groupsDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	name := "uwg_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + name).Error; err != nil {
		t.Fatalf("schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+name), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + name + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(
		&schema.UnofficialWhatsAppGroup{}, &schema.UnofficialWhatsAppConversation{}, &schema.ConversationMessage{},
		&schema.UnofficialWhatsAppContact{},
		&schema.UnofficialWhatsAppWorkspaceGroup{}, &schema.UnofficialWhatsAppGroupMember{},
		&schema.UnofficialWhatsAppGroupMemberEvent{}, &schema.UnofficialWhatsAppGroupAnalysis{},
		&schema.UnofficialWhatsAppGroupAnalysisRun{}, &schema.UnofficialWhatsAppGroupJoin{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE leads (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, number varchar(20), name varchar(255), deleted_at timestamptz)`,
		`CREATE UNIQUE INDEX ux_uw_workspace_group_jid ON unofficial_whatsapp_workspace_groups (workspace_id, jid) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX ux_uw_group_member_person ON unofficial_whatsapp_group_members (group_id, person_key)`,
		`CREATE UNIQUE INDEX ux_uw_group_analysis_run_cycle ON unofficial_whatsapp_group_analysis_runs (analysis_id, cycle)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return db
}

type world struct {
	db                   *gorm.DB
	ws, n1, n2           string
	groups               uwg.GroupRepository
	members              uwg.MemberRepository
	stats                uwg.StatsReader
	analyses             uwg.AnalysisRepository
	runs                 uwg.RunRepository
	joins                uwg.JoinRepository
	messages             uwg.MessageReader
	loc                  *time.Location
	now                  time.Time
	vila, ofertas, other *uwg.Group
}

func newWorld(t *testing.T) *world {
	db := groupsDB(t)
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	w := &world{
		db: db, ws: uuid.NewString(), n1: uuid.NewString(), n2: uuid.NewString(),
		groups: NewGroupRepository(db), members: NewMemberRepository(db), stats: NewStatsReader(db),
		analyses: NewAnalysisRepository(db), runs: NewRunRepository(db), joins: NewJoinRepository(db),
		messages: NewMessageReader(db), loc: loc, now: time.Date(2026, 10, 17, 15, 0, 0, 0, loc),
	}
	w.vila = w.followedGroup(t, "120363000000000001@g.us", "Moradores Vila Mariana", w.n1)
	w.ofertas = w.followedGroup(t, "120363000000000002@g.us", "Ofertas VIP #1", w.n2)
	w.other = w.seenGroup(t, "120363000000000003@g.us", "Pais da Escola", w.n2)
	return w
}

func (w *world) seat(t *testing.T, instanceID, jid, subject string) {
	t.Helper()
	if err := w.db.Create(&schema.UnofficialWhatsAppGroup{WorkspaceID: w.ws, InstanceID: instanceID, JID: jid, Subject: subject}).Error; err != nil {
		t.Fatal(err)
	}
}

func (w *world) seenGroup(t *testing.T, jid, subject, instanceID string) *uwg.Group {
	t.Helper()
	w.seat(t, instanceID, jid, subject)
	g := &uwg.Group{WorkspaceID: w.ws, JID: jid, Subject: subject}
	if err := w.groups.UpsertSeen(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	g.PrimaryInstanceID = instanceID
	if err := w.groups.SetPrimary(context.Background(), w.ws, g.ID, instanceID); err != nil {
		t.Fatal(err)
	}
	return g
}

func (w *world) followedGroup(t *testing.T, jid, subject, instanceID string) *uwg.Group {
	t.Helper()
	g := w.seenGroup(t, jid, subject, instanceID)
	at := w.now.Add(-60 * 24 * time.Hour)
	g.FollowedAt = &at
	if err := w.groups.SetFollowed(context.Background(), w.ws, g.ID, &at); err != nil {
		t.Fatal(err)
	}
	return g
}

func (w *world) conversation(t *testing.T, g *uwg.Group) string {
	t.Helper()
	last := w.now.Add(-time.Hour)
	conv := schema.UnofficialWhatsAppConversation{WorkspaceID: w.ws, InstanceID: g.PrimaryInstanceID, ContactID: uuid.NewString(), ChatID: g.JID, IsGroup: true, LastMessageAt: &last}
	if err := w.db.Create(&conv).Error; err != nil {
		t.Fatal(err)
	}
	return conv.ID
}

func (w *world) message(t *testing.T, convID, from, text, kind string, at time.Time) {
	t.Helper()
	if kind == "" {
		kind = "user_message"
	}
	if err := w.db.Create(&schema.ConversationMessage{EntryID: convID, EntryType: "unofficial_whatsapp", MessageType: kind, FromParticipant: from, Text: text, Direction: "INBOUND", CreatedAt: at}).Error; err != nil {
		t.Fatal(err)
	}
}

func roster(people ...string) []uw.GroupParticipant {
	out := make([]uw.GroupParticipant, 0, len(people))
	for _, p := range people {
		out = append(out, uw.GroupParticipant{JID: p + "@s.whatsapp.net", PhoneNumber: p, DisplayName: "P" + p[len(p)-2:], Role: uw.GroupRoleMember})
	}
	return out
}

func (w *world) applyRoster(t *testing.T, g *uwg.Group, at time.Time, baseline bool, people ...string) {
	t.Helper()
	ctx := context.Background()
	current, err := w.members.All(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	diff := uwg.DiffMembers(g.ID, w.ws, current, roster(people...), uwg.NewOwnNumbers([]string{"5511970001201"}, nil), at, baseline)
	if err := w.members.Apply(ctx, g, diff, at); err != nil {
		t.Fatal(err)
	}
}

func TestGroupDirectoryKeepsWhatPeopleSetAndHonoursScope(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	w.vila.Category, w.vila.Origin = "Bairro", uwg.OriginExternal
	if err := w.groups.Save(ctx, w.vila); err != nil {
		t.Fatal(err)
	}
	seenAgain := &uwg.Group{WorkspaceID: w.ws, JID: w.vila.JID, Subject: "Moradores VM", MemberCount: 290}
	if err := w.groups.UpsertSeen(ctx, seenAgain); err != nil {
		t.Fatal(err)
	}
	if seenAgain.Category != "Bairro" || seenAgain.FollowedAt == nil || seenAgain.Subject != "Moradores VM" || seenAgain.MemberCount != 0 {
		t.Fatalf("re-seen group = %+v, want the name refreshed, category and follow kept, and the member count left to the roster sync", seenAgain)
	}
	unfollowed := &uwg.Group{WorkspaceID: w.ws, JID: w.other.JID, Subject: w.other.Subject, MemberCount: 41}
	if err := w.groups.UpsertSeen(ctx, unfollowed); err != nil {
		t.Fatal(err)
	}
	if unfollowed.MemberCount != 41 {
		t.Fatalf("unfollowed group count = %d, want the provider count", unfollowed.MemberCount)
	}

	all, err := w.groups.List(ctx, uwg.GroupFilter{WorkspaceID: w.ws, Pagination: shared.Pagination{Page: 1, PageSize: 20}})
	if err != nil || all.TotalItems != 3 {
		t.Fatalf("unrestricted list = %v, %v", all, err)
	}
	onlyN1 := must(w.groups.List(ctx, uwg.GroupFilter{WorkspaceID: w.ws, Scope: uwg.Scope{Restrict: true, InstanceIDs: []string{w.n1}}}))
	if onlyN1.TotalItems != 1 || onlyN1.Items[0].ID != w.vila.ID {
		t.Fatalf("scoped list = %+v, want only the group of the scoped number", onlyN1.Items)
	}
	nothing := must(w.groups.List(ctx, uwg.GroupFilter{WorkspaceID: w.ws, Scope: uwg.Scope{Restrict: true, InstanceIDs: []string{}}}))
	if nothing.TotalItems != 0 {
		t.Fatalf("restricted scope without numbers listed %d groups", nothing.TotalItems)
	}
	if _, err := w.groups.FindByID(ctx, w.ws, w.ofertas.ID, uwg.Scope{Restrict: true, InstanceIDs: []string{w.n1}}); err != uwg.ErrGroupNotFound {
		t.Fatalf("out of scope FindByID = %v, want not found", err)
	}
	followed := true
	f := must(w.groups.List(ctx, uwg.GroupFilter{WorkspaceID: w.ws, Followed: &followed}))
	if f.TotalItems != 2 {
		t.Fatalf("followed = %d, want 2", f.TotalItems)
	}
	cats := must(w.groups.Categories(ctx, w.ws, uwg.Scope{}))
	if len(cats) != 1 || cats[0] != "Bairro" {
		t.Fatalf("categories = %v", cats)
	}
	if ok := must(w.groups.Exists(ctx, w.ws)); !ok {
		t.Fatalf("workspace with groups reads as empty")
	}
	if ok := must(w.groups.Exists(ctx, uuid.NewString())); ok {
		t.Fatalf("workspace without groups reads as having them")
	}
}

func TestMembershipHistoryLinksLeadsAndCountsPeopleAcrossGroups(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if err := w.db.Exec(`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '5511981124410', 'Rita Camargo')`, uuid.NewString(), w.ws).Error; err != nil {
		t.Fatal(err)
	}

	start := w.now.Add(-10 * 24 * time.Hour)
	w.applyRoster(t, w.vila, start, true, "551181124410", "5511992307781", "5511970001201")
	w.applyRoster(t, w.vila, w.now.Add(-2*24*time.Hour), false, "551181124410", "5511992307781", "5511970001201", "5511960213375")
	w.applyRoster(t, w.vila, w.now.Add(-24*time.Hour), false, "551181124410", "5511960213375", "5511970001201")
	w.applyRoster(t, w.ofertas, start, true, "551181124410", "5511960213375")

	counts, err := w.members.Counts(ctx, w.vila.ID, w.now.Add(-30*24*time.Hour))
	if err != nil || counts.Active != 2 || counts.Left30d != 1 || counts.WithLead != 1 {
		t.Fatalf("counts = %+v, %v; want 2 active people, 1 left, Rita linked through her 9th-digit form", counts, err)
	}
	g := must(w.groups.FindByID(ctx, w.ws, w.vila.ID, uwg.Scope{}))
	if g.MemberCount != 2 || g.MembersSyncedAt == nil {
		t.Fatalf("group member count = %d synced %v, want our number left out", g.MemberCount, g.MembersSyncedAt)
	}

	page, err := w.members.List(ctx, w.ws, uwg.MemberFilter{GroupID: w.vila.ID, Status: uwg.MemberActive})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]*uwg.MemberRow{}
	for _, m := range page.Items {
		byKey[m.PersonKey] = m
	}
	if byKey["551181124410"] == nil || byKey["551181124410"].LeadName != "Rita Camargo" || byKey["551181124410"].OtherGroups != 1 {
		t.Fatalf("rita = %+v, want lead name and one other group", byKey["551181124410"])
	}
	if !byKey["5511970001201"].OurNumber {
		t.Fatalf("our number not flagged in the list")
	}
	if err := w.db.Exec(`UPDATE unofficial_whatsapp_group_members SET display_name = '' WHERE group_id = ? AND person_key = ?`, w.vila.ID, "551181124410").Error; err != nil {
		t.Fatal(err)
	}
	if names := must(w.members.Names(ctx, w.vila.ID)); names["551181124410"] != "Rita Camargo" {
		t.Fatalf("names = %v, want the lead name when the member has no display name or contact", names)
	}
	if err := w.db.Create(&schema.UnofficialWhatsAppContact{WorkspaceID: w.ws, InstanceID: w.n1, JID: "551181124410@s.whatsapp.net",
		PhoneNumber: "551181124410", Name: "Rita C.", PictureURL: "https://files.test/rita.jpg"}).Error; err != nil {
		t.Fatal(err)
	}
	withContact := must(w.members.List(ctx, w.ws, uwg.MemberFilter{GroupID: w.vila.ID, Search: "551181124410"}))
	if len(withContact.Items) != 1 || withContact.Items[0].ContactName != "Rita C." || withContact.Items[0].PictureURL != "https://files.test/rita.jpg" {
		t.Fatalf("member with a known contact = %+v, want the contact name and picture", withContact.Items)
	}
	names := must(w.members.Names(ctx, w.vila.ID))
	if names["551181124410"] == "" {
		t.Fatalf("names = %v, want a name for every member with a display name or a contact", names)
	}
	named := must(w.members.List(ctx, w.ws, uwg.MemberFilter{GroupID: w.vila.ID, Search: "P75"}))
	if named.TotalItems != 1 {
		t.Fatalf("name search matched %d, want 1", named.TotalItems)
	}

	growth, err := w.stats.DailyGrowth(ctx, uwg.StatsScope{WorkspaceID: w.ws}, w.vila.ID, uwg.LastDays(30, w.now, w.loc), w.loc)
	if err != nil {
		t.Fatal(err)
	}
	var joins, leaves int64
	for _, d := range growth {
		joins += d.Joins
		leaves += d.Leaves
	}
	if joins != 1 || leaves != 1 {
		t.Fatalf("growth joins %d leaves %d, want the baseline left out", joins, leaves)
	}

	people, err := w.stats.CrossGroupPeople(ctx, uwg.PeopleFilter{StatsScope: uwg.StatsScope{WorkspaceID: w.ws}, MinGroups: 2})
	if err != nil || people.TotalItems != 2 {
		t.Fatalf("cross-group people = %+v, %v; want Rita and Camila", people, err)
	}
	if people.Items[0].GroupCount != 2 || len(people.Items[0].Groups) != 2 {
		t.Fatalf("person = %+v", people.Items[0])
	}
	scoped := must(w.stats.CrossGroupPeople(ctx, uwg.PeopleFilter{StatsScope: uwg.StatsScope{WorkspaceID: w.ws, Scope: uwg.Scope{Restrict: true, InstanceIDs: []string{w.n1}}}, MinGroups: 2}))
	if scoped.TotalItems != 0 {
		t.Fatalf("scoped cross-group people counted a group outside the scope")
	}
	totals := must(w.stats.PeopleTotals(ctx, uwg.StatsScope{WorkspaceID: w.ws}))
	if totals.Unique != 2 || totals.Memberships != 4 {
		t.Fatalf("people totals = %+v", totals)
	}
	refs := must(w.members.GroupsOf(ctx, w.ws, []string{"551181124410"}, uwg.Scope{}))
	if len(refs["551181124410"]) != 2 {
		t.Fatalf("groups of rita = %+v", refs)
	}
}

func TestMessageMetricsReadThePrimaryConversation(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	vila := w.conversation(t, w.vila)
	ofertas := w.conversation(t, w.ofertas)
	day := time.Date(2026, 10, 16, 10, 0, 0, 0, w.loc)
	w.message(t, vila, "+5511981124410", "Fila enorme", "", day)
	w.message(t, vila, "+5511992307781", "Verdade", "", day.Add(4*time.Minute))
	w.message(t, vila, "+5511992307781", "25 minutos", "", day.Add(5*time.Minute))
	w.message(t, vila, "+5511981124410", "👍", "reaction", day.Add(6*time.Minute))
	w.message(t, vila, "+5511960213375", "[imagem]", "", day.Add(7*time.Minute))
	w.message(t, ofertas, "Vendas 2", "Oferta de hoje", "", day)

	win := uwg.LastDays(7, w.now, w.loc)
	daily, err := w.stats.DailyMessages(ctx, uwg.StatsScope{WorkspaceID: w.ws}, win, w.loc)
	if err != nil || daily["2026-10-16"].Messages != 5 || daily["2026-10-16"].ActiveGroups != 2 {
		t.Fatalf("daily = %+v, %v", daily, err)
	}
	top := must(w.stats.TopGroupsByMessages(ctx, uwg.StatsScope{WorkspaceID: w.ws}, win, 5))
	if len(top) != 2 || top[0].GroupID != w.vila.ID || top[0].Count != 4 {
		t.Fatalf("top = %+v", top)
	}
	rt, err := w.stats.ResponseTimes(ctx, uwg.StatsScope{WorkspaceID: w.ws}, win, w.loc)
	if err != nil || rt["2026-10-16"].Samples != 2 || rt["2026-10-16"].AvgMinutes < 2.99 || rt["2026-10-16"].AvgMinutes > 3.01 {
		t.Fatalf("response = %+v, %v; want gaps of 4 and 2 minutes between different senders", rt, err)
	}

	lines, err := w.messages.TextAfter(ctx, vila, uwg.Cursor{At: day.Add(-time.Minute)}, 10)
	if err != nil || len(lines) != 3 {
		t.Fatalf("transcript = %+v, %v; want reactions and placeholders left out", lines, err)
	}
	w.message(t, vila, "+5511992307781", "Mesmo segundo", "", day)
	tied := must(w.messages.TextAfter(ctx, vila, uwg.Cursor{At: day.Add(-time.Minute)}, 10))
	rest := must(w.messages.TextAfter(ctx, vila, uwg.Cursor{At: tied[0].At, ID: tied[0].ID}, 10))
	if len(tied) != 4 || len(rest) != 3 || rest[0].ID != tied[1].ID {
		t.Fatalf("cursor after the first of two messages in one second = %+v, want the second one next", rest)
	}
	conv, err := w.messages.GroupConversation(ctx, w.vila.PrimaryInstanceID, w.vila.JID)
	if err != nil || conv != vila {
		t.Fatalf("GroupConversation = %s, %v", conv, err)
	}
	if _, err := w.messages.GroupConversation(ctx, w.n1, "120363999@g.us"); err != uwg.ErrNoGroupConversation {
		t.Fatalf("missing conversation error = %v", err)
	}

	w.applyRoster(t, w.vila, w.now.Add(-20*24*time.Hour), true, "5511981124410", "5511992307781")
	w.applyRoster(t, w.vila, w.now.Add(-3*24*time.Hour), false, "5511981124410")
	rows, err := w.stats.Membership(ctx, uwg.MembershipFilter{StatsScope: uwg.StatsScope{WorkspaceID: w.ws}, Window: win})
	if err != nil {
		t.Fatal(err)
	}
	var vilaStat *uwg.MembershipStat
	for i := range rows {
		if rows[i].GroupID == w.vila.ID {
			vilaStat = &rows[i]
		}
	}
	if vilaStat == nil || vilaStat.Members != 1 || vilaStat.Leaves != 1 || vilaStat.Base != 2 || vilaStat.Messages != 5 || vilaStat.LastActivity == nil {
		t.Fatalf("membership = %+v", vilaStat)
	}
	activity := must(w.stats.GroupActivity(ctx, w.ws, []string{w.vila.ID, w.other.ID}, win.From))
	if activity[w.vila.ID].Messages != 5 || activity[w.vila.ID].Leaves != 1 {
		t.Fatalf("activity = %+v", activity)
	}
	totals := must(w.stats.GroupTotals(ctx, uwg.StatsScope{WorkspaceID: w.ws}))
	if totals.Groups != 3 || totals.Followed != 2 || totals.Open != 2 {
		t.Fatalf("group totals = %+v", totals)
	}
}

func TestAnalysesClaimOnceAndRunsAggregate(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a := &uwg.Analysis{WorkspaceID: w.ws, GroupID: w.vila.ID, Name: "Monitor", Prompt: "Fila", Status: uwg.AnalysisActive,
		Trigger: uwg.TriggerMessageCount, MessagesPerCycle: 3, TrackedFields: []uwg.TrackedField{{Key: "mencoes", Type: uwg.FieldNumber}}}
	if err := w.analyses.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := w.analyses.AddPending(ctx, w.vila.ID, 2); err != nil {
		t.Fatal(err)
	}
	if due := must(w.analyses.ClaimNext(ctx, w.now, "w1", w.now.Add(time.Minute))); due != nil {
		t.Fatalf("claimed with 2 of 3 pending")
	}
	_ = w.analyses.AddPending(ctx, w.vila.ID, 1)
	claimed, err := w.analyses.ClaimNext(ctx, w.now, "w1", w.now.Add(time.Minute))
	if err != nil || claimed == nil || len(claimed.TrackedFields) != 1 {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	if again := must(w.analyses.ClaimNext(ctx, w.now, "w2", w.now.Add(time.Minute))); again != nil {
		t.Fatalf("a leased analysis was claimed twice")
	}
	if err := w.analyses.Extend(ctx, claimed.ID, "w2", w.now.Add(time.Hour)); err != ErrAnalysisLeaseLost {
		t.Fatalf("extend by another worker = %v, want lease lost", err)
	}
	if err := w.analyses.Extend(ctx, claimed.ID, "w1", w.now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	backoff := &uwg.Analysis{WorkspaceID: w.ws, GroupID: w.ofertas.ID, Name: "Em espera", Prompt: "x", Status: uwg.AnalysisActive, Trigger: uwg.TriggerMessageCount, MessagesPerCycle: 1}
	later := w.now.Add(10 * time.Minute)
	backoff.NextRunAt = &later
	if err := w.analyses.Create(ctx, backoff); err != nil {
		t.Fatal(err)
	}
	_ = w.analyses.AddPending(ctx, w.ofertas.ID, 5)
	if held := must(w.analyses.ClaimNext(ctx, w.now, "w9", w.now.Add(time.Minute))); held != nil {
		t.Fatalf("an analysis in backoff was claimed: %+v", held)
	}
	_ = w.analyses.Delete(ctx, w.ws, backoff.ID)
	_ = w.analyses.AddPending(ctx, w.vila.ID, 4)
	until := w.now
	claimed.AnalyzedUntil, claimed.AnalyzedUntilID, claimed.LastRunAt = &until, "m-3", &until
	claimed.RecordFailure(w.now)
	if err := w.analyses.Finish(ctx, claimed, "w2", 3); err != ErrAnalysisLeaseLost {
		t.Fatalf("finish by another worker = %v, want lease lost", err)
	}
	if err := w.analyses.Finish(ctx, claimed, "w1", 3); err != nil {
		t.Fatal(err)
	}
	stored := must(w.analyses.FindByID(ctx, w.ws, a.ID))
	if stored.PendingMessages != 4 {
		t.Fatalf("pending = %d, want the 4 that arrived during the run kept", stored.PendingMessages)
	}

	completed := func(cycle int, s uwg.RunSentiment, alert uwg.AlertLevel, themes []string, at time.Time) *uwg.Run {
		return &uwg.Run{AnalysisID: a.ID, WorkspaceID: w.ws, GroupID: w.vila.ID, Cycle: cycle, Kind: uwg.RunCycle,
			Status: uwg.RunCompleted, MessagesAnalyzed: 10, StartedAt: at, CompletedAt: at,
			Result: &uwg.Result{Sentiment: s, AlertLevel: alert, KeyThemes: themes, Insights: []string{"Fila cresceu"},
				Participants: []uwg.ParticipantReading{{Name: "Rita", Sentiment: uwg.SentimentNegative, Messages: 6}, {Name: "Jorge", Sentiment: uwg.SentimentPositive, Messages: 4}}}}
	}
	for _, r := range []*uwg.Run{
		completed(1, uwg.SentimentNegative, uwg.AlertHigh, []string{"Fila do caixa", "Preço"}, w.now.Add(-48*time.Hour)),
		completed(2, uwg.SentimentMixed, uwg.AlertLow, []string{"fila do caixa"}, w.now.Add(-24*time.Hour)),
		{AnalysisID: a.ID, WorkspaceID: w.ws, GroupID: w.vila.ID, Cycle: 3, Kind: uwg.RunCycle, Status: uwg.RunFailed, StartedAt: w.now, CompletedAt: w.now, FailureReason: "provider"},
	} {
		if err := w.runs.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if next := must(w.runs.NextCycle(ctx, a.ID)); next != 4 {
		t.Fatalf("next cycle = %d", next)
	}
	totals := must(w.runs.Totals(ctx, uwg.StatsScope{WorkspaceID: w.ws}))
	if totals.Runs != 2 || totals.Messages != 20 || totals.HighAlerts != 1 {
		t.Fatalf("run totals = %+v", totals)
	}
	tally := must(w.runs.SentimentTally(ctx, uwg.StatsScope{WorkspaceID: w.ws}, w.now.Add(-7*24*time.Hour)))
	if tally.Negative != 12 || tally.Positive != 8 {
		t.Fatalf("tally = %+v", tally)
	}
	byGroup := must(w.runs.SentimentByGroup(ctx, uwg.StatsScope{WorkspaceID: w.ws}, w.now.Add(-7*24*time.Hour), 10))
	if len(byGroup) != 1 || byGroup[0].Negative != 12 {
		t.Fatalf("by group = %+v", byGroup)
	}
	themes, err := w.runs.Themes(ctx, uwg.ThemeFilter{StatsScope: uwg.StatsScope{WorkspaceID: w.ws}, Since: w.now.Add(-7 * 24 * time.Hour)})
	if err != nil || themes.TotalItems != 2 || themes.Items[0].Occurrences != 2 || len(themes.Items[0].Groups) != 1 {
		t.Fatalf("themes = %+v, %v", themes, err)
	}
	insights, err := w.runs.RecentInsights(ctx, uwg.StatsScope{WorkspaceID: w.ws}, 10)
	if err != nil || len(insights) != 2 || insights[0].AnalysisName != "Monitor" {
		t.Fatalf("insights = %+v, %v", insights, err)
	}
	recent := must(w.runs.RecentForGroups(ctx, w.ws, []string{w.vila.ID}, w.now.Add(-30*24*time.Hour), 2))
	if len(recent[w.vila.ID]) != 2 {
		t.Fatalf("recent = %d, want the per-group cap", len(recent[w.vila.ID]))
	}
	withSender := completed(9, uwg.SentimentNeutral, uwg.AlertLow, nil, w.now.Add(-72*time.Hour))
	withSender.Result.Participants = append(withSender.Result.Participants, uwg.ParticipantReading{Name: "dba9547c-89dd-43db-8911-166fdc8cec8c", Sentiment: uwg.SentimentNeutral, Messages: 1})
	if err := w.runs.Create(ctx, withSender); err != nil {
		t.Fatal(err)
	}
	if read := must(w.runs.FindByID(ctx, w.ws, a.ID, withSender.ID)); len(read.Result.Participants) != 2 {
		t.Fatalf("participants read back = %+v, want the sender id of our own message left out", read.Result.Participants)
	}
	if err := w.db.Exec(`DELETE FROM unofficial_whatsapp_group_analysis_runs WHERE id = ?`, withSender.ID).Error; err != nil {
		t.Fatal(err)
	}
	history := must(w.runs.ListByAnalysis(ctx, w.ws, a.ID, shared.Pagination{Page: 1, PageSize: 10}))
	if history.TotalItems != 3 || history.Items[0].Cycle != 3 {
		t.Fatalf("history = %+v", history)
	}
	scheduled := &uwg.Analysis{WorkspaceID: w.ws, GroupID: w.ofertas.ID, Name: "Diário", Prompt: "x", Status: uwg.AnalysisActive,
		Trigger: uwg.TriggerScheduled, MessagesPerCycle: 50, ScheduleHour: 18}
	next := w.now.Add(-time.Minute)
	scheduled.NextRunAt = &next
	if err := w.analyses.Create(ctx, scheduled); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		if next := must(w.analyses.ClaimNext(ctx, w.now.Add(time.Hour), "w3", w.now.Add(2*time.Hour))); next != nil {
			seen[next.ID] = true
		}
	}
	if len(seen) != 2 || !seen[scheduled.ID] || !seen[a.ID] {
		t.Fatalf("claimed %v, want the due schedule and the count analysis once its backoff passed", seen)
	}
	list, err := w.analyses.List(ctx, uwg.AnalysisFilter{WorkspaceID: w.ws})
	if err != nil || list.TotalItems != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if total, active, err := w.analyses.Counts(ctx, uwg.StatsScope{WorkspaceID: w.ws}); err != nil || total != 2 || active != 2 {
		t.Fatalf("counts = %d / %d", total, active)
	}
}

func TestJoinTasksAreClaimedOnceAndRemainingOnesFailTogether(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	tasks, _ := uwg.PlanJoins(uwg.JoinPlanInput{PlanID: uuid.NewString(), WorkspaceID: w.ws, InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		InstanceIDs: []string{w.n1, w.n2}}, w.now)
	if err := w.joins.CreateMany(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	first, err := w.joins.ClaimNext(ctx, w.now, "w1", w.now.Add(time.Minute))
	if err != nil || first == nil || first.Sequence != 0 {
		t.Fatalf("claim = %+v, %v; want only the anchor due now", first, err)
	}
	if again := must(w.joins.ClaimNext(ctx, w.now, "w2", w.now.Add(time.Minute))); again != nil {
		t.Fatalf("a claimed join was claimed again")
	}
	expired := must(w.joins.ClaimNext(ctx, w.now.Add(2*time.Minute), "w2", w.now.Add(3*time.Minute)))
	if expired == nil || expired.ID != first.ID {
		t.Fatalf("an expired lease was not reclaimed: %+v", expired)
	}
	first.Status = uwg.JoinDone
	if err := w.joins.Save(ctx, first); err != ErrJoinLeaseLost {
		t.Fatalf("save by the worker that lost the lease = %v, want lease lost", err)
	}
	expired.Status, expired.Error = uwg.JoinFailed, "revoked"
	if err := w.joins.Save(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if err := w.joins.FailRemaining(ctx, expired.PlanID, "invite revoked"); err != nil {
		t.Fatal(err)
	}
	recent := must(w.joins.Recent(ctx, w.ws, uwg.Scope{}, w.now.Add(-time.Hour)))
	for _, task := range recent {
		if task.Status != uwg.JoinFailed {
			t.Fatalf("task %d = %s, want the whole plan failed", task.Sequence, task.Status)
		}
	}
	scoped := must(w.joins.Recent(ctx, w.ws, uwg.Scope{Restrict: true, InstanceIDs: []string{w.n1}}, w.now.Add(-time.Hour)))
	if len(scoped) != 1 {
		t.Fatalf("scoped joins = %d, want only the scoped number", len(scoped))
	}

	plan, _ := uwg.PlanJoins(uwg.JoinPlanInput{PlanID: uuid.NewString(), WorkspaceID: w.ws, InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		InstanceIDs: []string{w.n1, w.n2}}, w.now)
	if err := w.joins.CreateMany(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if n := must(w.joins.CancelPlan(ctx, w.ws, plan[0].PlanID, uwg.Scope{Restrict: true, InstanceIDs: []string{w.n2}})); n != 1 {
		t.Fatalf("scoped cancel = %d, want only the n2 task", n)
	}
	if n := must(w.joins.CancelPlan(ctx, w.ws, plan[0].PlanID, uwg.Scope{Restrict: true, InstanceIDs: []string{}})); n != 0 {
		t.Fatalf("cancel with an empty scope = %d, want nothing", n)
	}
}

func TestGroupCampaignsReadTheGroupEntriesOfUnofficialCampaigns(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if err := w.db.AutoMigrate(&schema.UnofficialWhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaignEntry{}); err != nil {
		t.Fatal(err)
	}
	running := schema.UnofficialWhatsAppCampaign{WorkspaceID: w.ws, InstanceID: w.n1, Name: "Ofertas", Audience: "groups", Status: "RUNNING"}
	done := schema.UnofficialWhatsAppCampaign{WorkspaceID: w.ws, InstanceID: w.n1, Name: "Antiga", Audience: "groups", Status: "COMPLETED"}
	leads := schema.UnofficialWhatsAppCampaign{WorkspaceID: w.ws, InstanceID: w.n1, Name: "Leads", Status: "RUNNING"}
	for _, c := range []*schema.UnofficialWhatsAppCampaign{&running, &done, &leads} {
		if err := w.db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	leadID := uuid.NewString()
	if err := w.db.Exec(`ALTER TABLE unofficial_whatsapp_campaign_entries DROP CONSTRAINT IF EXISTS fk_unofficial_whatsapp_campaign_entries_lead`).Error; err != nil {
		t.Fatal(err)
	}
	for _, e := range []schema.UnofficialWhatsAppCampaignEntry{
		{CampaignID: running.ID, WorkspaceID: w.ws, JID: w.vila.JID, Name: "Vila", Status: "PENDING"},
		{CampaignID: done.ID, WorkspaceID: w.ws, JID: w.vila.JID, Name: "Vila", Status: "SENT"},
		{CampaignID: leads.ID, WorkspaceID: w.ws, LeadID: &leadID, Number: "5511981124410", JID: "5511981124410@s.whatsapp.net", Status: "PENDING"},
	} {
		if err := w.db.Create(&e).Error; err != nil {
			t.Fatal(err)
		}
	}
	reader := NewCampaignReader(w.db)
	rows, err := reader.ForGroup(ctx, w.ws, w.vila.JID, uwg.Scope{}, 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("group campaigns = %+v, %v", rows, err)
	}
	counts := must(reader.RunningForGroups(ctx, w.ws, []string{w.vila.JID, w.ofertas.JID}, uwg.Scope{}))
	if counts[w.vila.JID] != 1 || counts[w.ofertas.JID] != 0 {
		t.Fatalf("running = %+v", counts)
	}
	otherDepartment := uwg.Scope{Restrict: true, InstanceIDs: []string{w.n2}}
	if hidden := must(reader.ForGroup(ctx, w.ws, w.vila.JID, otherDepartment, 10)); len(hidden) != 0 {
		t.Fatalf("campaigns of another department's number = %+v, want none", hidden)
	}
	if hidden := must(reader.RunningForGroups(ctx, w.ws, []string{w.vila.JID}, otherDepartment)); hidden[w.vila.JID] != 0 {
		t.Fatalf("running count leaked across departments: %+v", hidden)
	}
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
