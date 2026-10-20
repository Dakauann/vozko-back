package crmtools

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
	uwguc "vozko/usecases/unofficial_whatsapp_group"
)

const (
	knownGroupA = "81920314-c5d6-4e7f-9081-9c0d1e2f3a4b"
	knownGroupB = "92031425-d6e7-4f80-8192-0d1e2f3a4b5c"
)

type fakeGroupDirectory struct {
	actor    uwguc.Actor
	list     uwguc.ListGroupsInput
	analysis uwguc.AnalysisInput
	rows     map[string]*uwguc.GroupRow
}

func (f *fakeGroupDirectory) Dashboard(_ context.Context, a uwguc.Actor) (*uwguc.DashboardStats, error) {
	f.actor = a
	return &uwguc.DashboardStats{
		Groups:      uwg.GroupTotals{Groups: 4, Followed: 3},
		People:      uwg.PeopleTotals{Memberships: 900, Unique: 640},
		DailyVolume: []uwg.DailyPoint{{Day: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), Messages: 120, ActiveGroups: 2}},
		Growth:      []uwg.DailyPoint{{Day: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), Joins: 9, Leaves: 2, Net: 7}},
		Sentiment:   uwg.SentimentTally{Positive: 30, Neutral: 50, Negative: 20},
	}, nil
}

func (f *fakeGroupDirectory) ListGroups(_ context.Context, a uwguc.Actor, in uwguc.ListGroupsInput) (*shared.PaginatedResult[*uwguc.GroupRow], error) {
	f.actor, f.list = a, in
	return &shared.PaginatedResult[*uwguc.GroupRow]{Items: []*uwguc.GroupRow{f.rows[knownGroupA]}, TotalItems: 1, Page: 1, PageSize: 20, TotalPages: 1}, nil
}

func (f *fakeGroupDirectory) GroupsByID(_ context.Context, a uwguc.Actor, ids []string) ([]*uwguc.GroupRow, error) {
	f.actor = a
	out := make([]*uwguc.GroupRow, 0, len(ids))
	for _, id := range ids {
		row, ok := f.rows[id]
		if !ok {
			return nil, uwg.ErrGroupNotFound
		}
		out = append(out, row)
	}
	return out, nil
}

func (f *fakeGroupDirectory) GetGroup(_ context.Context, a uwguc.Actor, id string) (*uwguc.GroupDetail, error) {
	f.actor = a
	row, ok := f.rows[id]
	if !ok {
		return nil, uwg.ErrGroupNotFound
	}
	return &uwguc.GroupDetail{GroupRow: *row, Members: uwg.MemberCounts{Active: 312, Left30d: 14, WithLead: 80}}, nil
}

func (f *fakeGroupDirectory) People(_ context.Context, a uwguc.Actor, _ string, minGroups int, _ shared.Pagination) (*shared.PaginatedResult[*uwg.Person], error) {
	f.actor = a
	return &shared.PaginatedResult[*uwg.Person]{Items: []*uwg.Person{{Name: "Ana", Phone: "5584994409624", GroupCount: minGroups,
		Groups: []uwg.GroupRef{{GroupID: knownGroupA, Subject: "Moradores"}, {GroupID: knownGroupB, Subject: "Ofertas"}}}}, TotalItems: 1}, nil
}

func (f *fakeGroupDirectory) CreateAnalysis(_ context.Context, a uwguc.Actor, in uwguc.AnalysisInput) (*uwg.Analysis, error) {
	f.actor, f.analysis = a, in
	return &uwg.Analysis{ID: "a-1", Name: in.Name, GroupID: in.GroupID, Trigger: in.Trigger, ScheduleHour: in.ScheduleHour}, nil
}

func (f *fakeGroupDirectory) DraftAnalysis(a uwguc.Actor, in uwguc.AnalysisInput) (*uwg.Analysis, error) {
	draft := &uwg.Analysis{WorkspaceID: a.WorkspaceID, GroupID: in.GroupID, Name: in.Name, Prompt: in.Prompt, Trigger: in.Trigger,
		MessagesPerCycle: in.MessagesPerCycle, ScheduleHour: in.ScheduleHour, ScheduleMinute: in.ScheduleMinute}
	draft.Normalize()
	return draft, draft.Validate()
}

type fakeGroupAccess struct{ denied map[workspace.Resource]bool }

func (f fakeGroupAccess) Execute(_, _ string, resource workspace.Resource, _ workspace.Action) error {
	if f.denied[resource] {
		return workspace.ErrResourceAccessDenied
	}
	return nil
}

func groupRow(id, subject string, canPost bool) *uwguc.GroupRow {
	followed := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return &uwguc.GroupRow{
		Group:   &uwg.Group{ID: id, JID: "1203630000000000" + id[:2] + "@g.us", Subject: subject, MemberCount: 312, Origin: uwg.OriginExternal, FollowedAt: &followed},
		Numbers: []uwguc.NumberSeat{{InstanceID: knownQRNumber, Label: "Loja Centro", Connected: true, CanPost: canPost}},
	}
}

type fakeGroupEstimate struct {
	directory *fakeGroupDirectory
	scopes    []uw.DepartmentScope
}

func (f *fakeGroupEstimate) Draft(_ context.Context, in uwc.EstimateInput, scope uw.DepartmentScope) (*uwc.DraftEstimate, error) {
	f.scopes = append(f.scopes, scope)
	draft := &uwc.DraftEstimate{}
	for _, jid := range in.GroupJIDs {
		group := uwc.GroupSenders{JID: jid, Senders: []uwc.SenderNumber{}, Block: uwg.SenderNotInGroup}
		for _, row := range f.directory.rows {
			if row.Group.JID != jid {
				continue
			}
			group.Name, group.Block = row.Group.Subject, uwg.SenderCannotPost
			for _, seat := range row.Numbers {
				if seat.CanPost {
					group.Senders = append(group.Senders, uwc.SenderNumber{InstanceID: seat.InstanceID, Label: seat.Label})
					group.Block = uwg.SenderReady
				}
			}
		}
		if group.Block == uwg.SenderReady {
			draft.Estimate.Pending++
			draft.Estimate.Duration += 90 * time.Second
		}
		draft.Groups = append(draft.Groups, group)
	}
	return draft, nil
}

func (f *fakeGroupEstimate) Remaining(context.Context, *uwc.Campaign) (*uwc.SendEstimate, error) {
	return &uwc.SendEstimate{}, nil
}

type groupFixture struct {
	deps      UnofficialGroupDeps
	directory *fakeGroupDirectory
	create    *fakeQRCreate
	estimate  *fakeGroupEstimate
}

func newGroupFixture() groupFixture {
	qr := newQRFixture()
	directory := &fakeGroupDirectory{rows: map[string]*uwguc.GroupRow{
		knownGroupA: groupRow(knownGroupA, "Moradores", true),
		knownGroupB: groupRow(knownGroupB, "Ofertas", false),
	}}
	estimate := &fakeGroupEstimate{directory: directory}
	return groupFixture{
		deps:      UnofficialGroupDeps{Campaigns: qr.deps, Groups: directory, Permissions: fakeGroupAccess{}, Estimate: estimate},
		directory: directory,
		create:    qr.create,
		estimate:  estimate,
	}
}

func TestGroupToolsDeclareTheirPermissions(t *testing.T) {
	deps := newGroupFixture().deps
	cases := []struct {
		tool             copilot.Tool
		resource, action string
		mutating         bool
	}{
		{NewUnofficialGroupsDashboardTool(deps), "unofficial_whatsapp_groups", "read", false},
		{NewListUnofficialGroupsTool(deps), "unofficial_whatsapp_groups", "read", false},
		{NewUnofficialGroupOverviewTool(deps), "unofficial_whatsapp_groups", "read", false},
		{NewUnofficialGroupPeopleTool(deps), "unofficial_whatsapp_groups", "read", false},
		{NewCreateUnofficialGroupCampaignTool(deps), "unofficial_whatsapp_campaigns", "create", true},
		{NewCreateUnofficialGroupAnalysisTool(deps), "unofficial_whatsapp_groups", "configure", true},
	}
	for _, c := range cases {
		m := c.tool.Meta()
		_, validates := c.tool.(copilot.Validator)
		_, describes := c.tool.(copilot.Describer)
		if string(m.Resource) != c.resource || string(m.Action) != c.action || m.Mutating != c.mutating {
			t.Fatalf("%s meta = %+v", c.tool.Definition().Name, m)
		}
		if c.mutating && (!validates || !describes) {
			t.Fatalf("%s changes something without a preflight and a card", c.tool.Definition().Name)
		}
	}
}

func TestGroupReadsRunWithTheUsersDepartmentScope(t *testing.T) {
	f := newGroupFixture()
	tools := []copilot.Tool{
		NewUnofficialGroupsDashboardTool(f.deps), NewListUnofficialGroupsTool(f.deps),
		NewUnofficialGroupPeopleTool(f.deps),
	}
	for _, tool := range tools {
		f.directory.actor = uwguc.Actor{}
		res := tool.Execute(context.Background(), tooltest.Member(), nil)
		if res.Status != copilot.StatusOK || !f.directory.actor.Departments.Restrict || f.directory.actor.WorkspaceID != "ws-1" || f.directory.actor.UserID != "u-1" {
			t.Fatalf("%s result %+v actor %+v", tool.Definition().Name, res, f.directory.actor)
		}
	}
	overview := NewUnofficialGroupOverviewTool(f.deps).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"group_id": knownGroupA})
	if overview.Status != copilot.StatusOK || !f.directory.actor.Departments.Restrict {
		t.Fatalf("overview = %+v", overview)
	}

	f.deps.Campaigns.Scopes = fakeScopes{denied: true}
	denied := []copilot.Tool{
		NewUnofficialGroupsDashboardTool(f.deps), NewListUnofficialGroupsTool(f.deps),
		NewUnofficialGroupPeopleTool(f.deps), NewUnofficialGroupOverviewTool(f.deps),
	}
	for _, tool := range denied {
		if res := tool.Execute(context.Background(), tooltest.Member(), map[string]interface{}{"group_id": knownGroupA}); res.Status == copilot.StatusOK {
			t.Fatalf("%s answered a user without access to the numbers", tool.Definition().Name)
		}
	}
}

func TestTheDashboardHandsChartsTheirDatasets(t *testing.T) {
	f := newGroupFixture()
	res := NewUnofficialGroupsDashboardTool(f.deps).Execute(context.Background(), tooltest.Member(), nil)
	data := tooltest.DataOf(res)
	for _, key := range []string{"daily_volume", "growth"} {
		if _, ok := data[key].(copilot.DatasetPreview); !ok {
			t.Fatalf("%s = %#v, want a dataset handle", key, data[key])
		}
	}
	if data["followed_groups"] != int64(3) || data["unique_people"] != int64(640) {
		t.Fatalf("totals = %+v", data)
	}
}

func TestAnUnknownGroupIsReportedAsNotFound(t *testing.T) {
	f := newGroupFixture()
	res := NewUnofficialGroupOverviewTool(f.deps).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"group_id": "a3142536-e7f8-4091-9203-1e2f3a4b5c6d"})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "grupo") {
		t.Fatalf("result = %+v", res)
	}
}

func groupCampaignInput(changes map[string]interface{}) map[string]interface{} {
	return withArgs(map[string]interface{}{
		"name": "Ofertas da semana", "group_ids": []interface{}{knownGroupA}, "message": "Chegou a promoção da semana!",
	}, changes)
}

func TestAGroupCampaignIsCreatedStoppedWithTheGroupsAsTargets(t *testing.T) {
	f := newGroupFixture()
	tool := NewCreateUnofficialGroupCampaignTool(f.deps)
	if err := tooltest.Validate(t, tool, tooltest.Member(), groupCampaignInput(nil)); err != nil {
		t.Fatal(err)
	}
	cc := tooltest.Member()
	res := tool.Execute(creating(cc), cc, groupCampaignInput(nil))
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	got := f.create.got
	if got.Audience != uwc.AudienceGroups || got.InstanceID != "" || len(got.Targets) != 1 || got.Targets[0].GroupJID != f.directory.rows[knownGroupA].Group.JID {
		t.Fatalf("campaign = %+v", got)
	}
	if got.Message.Bodies[0] != "Chegou a promoção da semana!" || !f.create.scope.Restrict {
		t.Fatalf("message %+v scope %+v", got.Message, f.create.scope)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), tooltest.Member(), groupCampaignInput(nil))
	if !tooltest.HasField(fields, "groups", "Moradores (Loja Centro)") || !tooltest.HasField(fields, "sendTime", "cerca de 2 min") {
		t.Fatalf("card = %+v", fields)
	}
	if len(f.estimate.scopes) == 0 || !f.estimate.scopes[0].Restrict {
		t.Fatalf("the senders were planned without the user's department scope: %+v", f.estimate.scopes)
	}
}

func TestAGroupCampaignFailsClosedWithoutTheSenderPlan(t *testing.T) {
	f := newGroupFixture()
	f.deps.Estimate = nil
	tool := NewCreateUnofficialGroupCampaignTool(f.deps)
	if err := tooltest.Validate(t, tool, tooltest.Member(), groupCampaignInput(nil)); err == nil {
		t.Fatal("a group campaign was prepared without knowing who posts in each group")
	}
	cc := tooltest.Member()
	if res := tool.Execute(creating(cc), cc, groupCampaignInput(nil)); res.Status == copilot.StatusOK || f.create.got != nil {
		t.Fatalf("result = %+v", res)
	}
}

func TestAGroupCampaignRefusesGroupsNoNumberCanPostIn(t *testing.T) {
	f := newGroupFixture()
	tool := NewCreateUnofficialGroupCampaignTool(f.deps)
	err := tooltest.Validate(t, tool, tooltest.Member(), groupCampaignInput(map[string]interface{}{"group_ids": []interface{}{knownGroupA, knownGroupB}}))
	if err == nil || !strings.Contains(err.Error(), "Ofertas") {
		t.Fatalf("validate = %v, want the group where only admins post named", err)
	}
	if err := tooltest.Validate(t, tool, tooltest.Member(), groupCampaignInput(map[string]interface{}{"group_ids": []interface{}{"a3142536-e7f8-4091-9203-1e2f3a4b5c6d"}})); err == nil {
		t.Fatal("a group outside the scope was accepted")
	}
}

func TestAGroupCampaignNeedsToSeeTheGroups(t *testing.T) {
	f := newGroupFixture()
	f.deps.Permissions = fakeGroupAccess{denied: map[workspace.Resource]bool{workspace.ResourceUnofficialWhatsAppGroups: true}}
	tool := NewCreateUnofficialGroupCampaignTool(f.deps)
	if err := tooltest.Validate(t, tool, tooltest.Member(), groupCampaignInput(nil)); err == nil {
		t.Fatal("a user who cannot see groups prepared a group campaign")
	}
	cc := tooltest.Member()
	if res := tool.Execute(creating(cc), cc, groupCampaignInput(nil)); res.Status == copilot.StatusOK || f.create.got != nil {
		t.Fatalf("result = %+v", res)
	}
	f.deps.Permissions = nil
	if res := NewCreateUnofficialGroupCampaignTool(f.deps).Execute(creating(cc), cc, groupCampaignInput(nil)); res.Status == copilot.StatusOK {
		t.Fatal("an unwired permission check let the campaign through")
	}
}

func TestAGroupAnalysisIsCreatedWithTheChosenTrigger(t *testing.T) {
	f := newGroupFixture()
	tool := NewCreateUnofficialGroupAnalysisTool(f.deps)
	args := map[string]interface{}{"group_id": knownGroupA, "name": "Reclamações", "prompt": "Reclamações sobre entrega", "trigger": "scheduled", "schedule_hour": 20}
	if err := tooltest.Validate(t, tool, tooltest.Member(), args); err != nil {
		t.Fatal(err)
	}
	res := tool.Execute(context.Background(), tooltest.Member(), args)
	if res.Status != copilot.StatusOK || f.directory.analysis.Trigger != uwg.TriggerScheduled || f.directory.analysis.ScheduleHour != 20 || f.directory.analysis.GroupID != knownGroupA {
		t.Fatalf("result %+v input %+v", res, f.directory.analysis)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), tooltest.Member(), args)
	if !tooltest.HasField(fields, "group", "Moradores") || tooltest.FieldValue(fields, "when") == "" || tooltest.FieldValue(fields, "cost") == "" {
		t.Fatalf("card = %+v", fields)
	}
	if err := tooltest.Validate(t, tool, tooltest.Member(), withArgs(args, map[string]interface{}{"trigger": "hourly"})); err == nil {
		t.Fatal("an unknown trigger was accepted")
	}
}
