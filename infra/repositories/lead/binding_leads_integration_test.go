package lead

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/domain/customfield"
	"vozko/infra/repositories/repotest"
)

func TestBindingLeadsCarryEveryFieldAVariableReadsLikeTheFullLead(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_binding")
	migrateLeadTables(t, db)
	ws, other := uuid.NewString(), uuid.NewString()
	full, phoneName, foreign, gone := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	owner := uuid.NewString()
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO leads (id, workspace_id, number, name, nickname, email, owner_id, owner_kind, custom_fields, birth_date, blocked, version, created_at, updated_at)
		VALUES (?, ?, '5511987650001', 'Ana Maria Lima', 'Aninha', 'ana@escola.example', ?, 'user', '{"serie":"1º ano","turmas":["A","B"]}', '2010-05-04', true, 3, now(), now())`, full, ws, owner)
	exec(`INSERT INTO leads (id, workspace_id, number, name, version, created_at, updated_at) VALUES (?, ?, '5511987650002', '+55 11 98765-0002', 1, now(), now())`, phoneName, ws)
	exec(`INSERT INTO leads (id, workspace_id, number, name, version, created_at, updated_at) VALUES (?, ?, '5511987650003', 'Outra', 1, now(), now())`, foreign, other)
	exec(`INSERT INTO leads (id, workspace_id, number, name, version, created_at, updated_at, deleted_at) VALUES (?, ?, '5511987650004', 'Saiu', 1, now(), now(), ?)`, gone, ws, time.Now())

	repo := &repository{db: db}
	ids := []string{full, phoneName, foreign, gone, "not-a-uuid"}
	whole, err := repo.FindByIDs(ws, ids)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := repo.BindingLeads(context.Background(), ws, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(bound) != 2 || len(whole) != 2 {
		t.Fatalf("binding leads = %d, full leads = %d, want the 2 live leads of the workspace", len(bound), len(whole))
	}
	defs := []*customfield.Definition{{Key: "serie", ObjectType: customfield.ObjectLead, Type: customfield.TypeText}, {Key: "turmas", ObjectType: customfield.ObjectLead, Type: customfield.TypeMultiSelect}}
	var bindings []campaign.VariableBinding
	for _, source := range campaign.BindableSources() {
		bindings = append(bindings, campaign.VariableBinding{Source: source})
	}
	bindings = append(bindings, campaign.VariableBinding{Source: campaign.CustomBinding("serie")}, campaign.VariableBinding{Source: campaign.CustomBinding("turmas")})
	plan, err := campaign.PlanBindings(bindings, len(bindings), defs)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	for i, l := range whole {
		byID[l.ID] = i
	}
	for _, l := range bound {
		w := whole[byID[l.ID]]
		got, want := plan.Values(campaign.BindingSubject{Lead: l, OwnerName: "Dona"}), plan.Values(campaign.BindingSubject{Lead: w, OwnerName: "Dona"})
		if len(got) != len(want) {
			t.Fatalf("values %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("lead %s slot %d (%s) = %q, the full lead gives %q", l.Name, i+1, plan.SourceOf(i+1), got[i], want[i])
			}
		}
		if l.Number != w.Number || l.Name != w.Name || l.WorkspaceID != w.WorkspaceID || l.Owner != w.Owner {
			t.Fatalf("binding lead %+v, full lead %+v", l, w)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.BindingLeads(ctx, ws, ids); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled request = %v", err)
	}
}
