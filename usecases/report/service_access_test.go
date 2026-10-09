package report_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"vozko/domain/report"
	"vozko/domain/workspace"
)

type grants map[string]map[string]bool

func (g grants) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return g[userID][resource+":"+action]
}

var (
	leadsRead   = workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}
	leadsExport = workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionExport}
)

func leadsRenderer() *stubRenderer {
	r := okRenderer()
	r.kind = report.KindLeads
	r.internal = true
	r.policy = report.Policy{Required: []workspace.PermissionEntry{leadsRead, leadsExport}, Tier: "basic"}
	return r
}

func leadAccess() grants {
	return grants{
		"manager":       {"leads:read": true, "leads:export": true},
		"other-manager": {"leads:read": true, "leads:export": true},
		"analyst":       {"leads:read": true},
		"finance":       {"balance:read": true},
	}
}

func createLeads(t *testing.T, service *Service, requester string) (*report.Job, error) {
	t.Helper()
	return service.Create(CreateInput{
		WorkspaceID: "ws-1", RequestedBy: requester, Kind: report.KindLeads, Format: report.FormatCSV, Locale: "pt",
		Params: json.RawMessage(`{"snapshotId":"s-1"}`),
	})
}

func TestTheLeadsKindIsNotOfferedThroughTheGenericRoute(t *testing.T) {
	service, _, _, _ := buildService(t, leadsRenderer())
	service.SetAccess(leadAccess())

	_, err := service.Create(CreateInput{WorkspaceID: "ws-1", RequestedBy: "manager", Kind: report.KindLeads, Format: report.FormatCSV, FromClient: true})
	if !errors.Is(err, report.ErrKindNotOffered) {
		t.Fatalf("a client created a leads report: %v", err)
	}
	if _, err := createLeads(t, service, "manager"); err != nil {
		t.Fatalf("the leads action itself creates the report: %v", err)
	}
}

func TestCreatingAReportNeedsThePermissionsOfItsKind(t *testing.T) {
	service, _, _, _ := buildService(t, leadsRenderer())
	service.SetAccess(leadAccess())

	if _, err := createLeads(t, service, "analyst"); !errors.Is(err, report.ErrNotAllowed) {
		t.Fatalf("reports:create alone exported leads: %v", err)
	}
	if _, err := createLeads(t, service, ""); !errors.Is(err, report.ErrNotAllowed) {
		t.Fatalf("a report without a requester was created: %v", err)
	}
}

func TestAFinanceMemberIsDeniedAManagersLeadFile(t *testing.T) {
	service, repo, _, storage := buildService(t, leadsRenderer())
	service.SetAccess(leadAccess())
	job, err := createLeads(t, service, "manager")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Upload("k", []byte("x"), "text/csv"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkDone(job.ID, "k", "leads.csv", 1, 1, job.CreatedAt, nil); err != nil {
		t.Fatal(err)
	}

	finance := Viewer{UserID: "finance"}
	if _, err := service.Get(finance, "ws-1", job.ID); !errors.Is(err, report.ErrNotFound) {
		t.Fatalf("finance read the job: %v", err)
	}
	if _, err := service.File(context.Background(), finance, "ws-1", job.ID); !errors.Is(err, report.ErrNotFound) {
		t.Fatalf("finance downloaded the file: %v", err)
	}
	page, err := service.List(finance, report.ListQuery{WorkspaceID: "ws-1"})
	if err != nil || len(page.Jobs) != 0 || page.Total != 0 {
		t.Fatalf("finance listed %+v, %v", page, err)
	}
	if _, err := service.File(context.Background(), Viewer{UserID: "manager"}, "ws-1", job.ID); err != nil {
		t.Fatalf("the requester downloads its file: %v", err)
	}
	if _, err := service.File(context.Background(), Viewer{UserID: "other-manager"}, "ws-1", job.ID); err != nil {
		t.Fatalf("a holder of the same permissions downloads the file: %v", err)
	}
}

func TestTwoRequestersGetTwoJobs(t *testing.T) {
	service, repo, _, _ := buildService(t, leadsRenderer())
	service.SetAccess(leadAccess())

	first, err := createLeads(t, service, "manager")
	if err != nil {
		t.Fatal(err)
	}
	again, err := createLeads(t, service, "manager")
	if err != nil || again.ID != first.ID {
		t.Fatalf("the same requester repeating the request reuses the job: %v, %v", again, err)
	}
	second, err := createLeads(t, service, "other-manager")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || len(repo.created) != 2 {
		t.Fatalf("two requesters shared a job: %s and %s", first.ID, second.ID)
	}
}

func TestTheWorkerChecksThePermissionsAgainAtRender(t *testing.T) {
	renderer := leadsRenderer()
	service, repo, publisher, _ := buildService(t, renderer)
	access := leadAccess()
	service.SetAccess(access)
	job, err := createLeads(t, service, "manager")
	if err != nil {
		t.Fatal(err)
	}
	access["manager"]["leads:export"] = false

	ack := &fakeAck{deliveries: 1}
	NewWorker(service, nil).handle(publisher.published[0], ack)

	if renderer.calls != 0 {
		t.Fatal("a report was rendered for a requester who lost the permission")
	}
	if got := repo.failCode[job.ID]; got != report.FailureForbidden || !ack.acked {
		t.Fatalf("failure = %q, acked = %v", got, ack.acked)
	}
}

func TestAKindWithoutAPolicyIsRefused(t *testing.T) {
	renderer := okRenderer()
	renderer.unguarded = true
	service, _, _, _ := buildService(t, renderer)
	if _, err := service.Create(CreateInput{WorkspaceID: "ws-1", RequestedBy: "user-1", Kind: report.KindAttendanceOverview, Format: report.FormatCSV}); !errors.Is(err, report.ErrNoPolicy) {
		t.Fatalf("a kind without a policy was created: %v", err)
	}
}

func TestReportsRefuseWithoutAnAccessPort(t *testing.T) {
	service, _, _, _ := buildService(t, okRenderer())
	service.SetAccess(nil)
	if _, err := service.Create(CreateInput{WorkspaceID: "ws-1", RequestedBy: "user-1", Kind: report.KindAttendanceOverview, Format: report.FormatCSV}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("a report was created without the access port: %v", err)
	}
}

func scopedRenderer(kind report.Kind, required workspace.PermissionEntry) *stubRenderer {
	r := okRenderer()
	r.kind = kind
	r.policy = report.Policy{Required: []workspace.PermissionEntry{required}, RequesterOnly: true}
	return r
}

func TestAScopedMemberCannotReadAReportRenderedWithSomeoneElsesScope(t *testing.T) {
	cases := []struct {
		name     string
		kind     report.Kind
		required workspace.PermissionEntry
		owner    string
		member   string
	}{
		{"an owner's entries file and a department-scoped attendant", report.KindConversationEntries,
			workspace.PermissionEntry{Resource: workspace.ResourceWhatsAppCampaigns, Action: workspace.ActionRead}, "owner", "attendant"},
		{"a manager's deals file and a restricted seller", report.KindOpportunities,
			workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}, "manager", "seller"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, _, storage := buildService(t, scopedRenderer(tc.kind, tc.required))
			service.SetAccess(grants{tc.owner: {tc.required.Key(): true}, tc.member: {tc.required.Key(): true}})
			job, err := service.Create(CreateInput{WorkspaceID: "ws-1", RequestedBy: tc.owner, Kind: tc.kind, Format: report.FormatCSV, Params: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			if err := storage.Upload("k", []byte("x"), "text/csv"); err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkDone(job.ID, "k", "f.csv", 1, 1, job.CreatedAt, nil); err != nil {
				t.Fatal(err)
			}
			member := Viewer{UserID: tc.member}
			if _, err := service.File(context.Background(), member, "ws-1", job.ID); !errors.Is(err, report.ErrNotFound) {
				t.Fatalf("the member downloaded the file: %v", err)
			}
			page, err := service.List(member, report.ListQuery{WorkspaceID: "ws-1"})
			if err != nil || len(page.Jobs) != 0 || page.Total != 0 {
				t.Fatalf("the member listed %+v, %v", page, err)
			}
			if _, err := service.File(context.Background(), Viewer{UserID: tc.owner}, "ws-1", job.ID); err != nil {
				t.Fatalf("the requester downloads its file: %v", err)
			}
		})
	}
}

func TestListPagesAndCountsOnlyWhatTheViewerReads(t *testing.T) {
	service, _, _, _ := buildService(t, leadsRenderer())
	access := leadAccess()
	service.SetAccess(access)
	for i := 0; i < 5; i++ {
		access[requesterN(i)] = map[string]bool{"leads:read": true, "leads:export": true}
		if _, err := createLeads(t, service, requesterN(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := createLeads(t, service, "manager"); err != nil {
		t.Fatal(err)
	}

	finance := Viewer{UserID: "finance"}
	page, err := service.List(finance, report.ListQuery{WorkspaceID: "ws-1", Limit: 2})
	if err != nil || len(page.Jobs) != 0 || page.Total != 0 {
		t.Fatalf("finance page = %+v, %v", page, err)
	}
	other := Viewer{UserID: "other-manager"}
	page, err = service.List(other, report.ListQuery{WorkspaceID: "ws-1", Limit: 2})
	if err != nil || len(page.Jobs) != 2 || page.Total != 6 {
		t.Fatalf("a holder of the permissions page = %d jobs, total %d, %v", len(page.Jobs), page.Total, err)
	}
}

func requesterN(i int) string {
	return "manager-" + string(rune('a'+i))
}
