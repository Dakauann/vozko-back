package report

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/workspace"
)

type guardedRenderer struct {
	kind     Kind
	policy   Policy
	internal bool
}

func (r guardedRenderer) Kind() Kind        { return r.kind }
func (r guardedRenderer) Formats() []Format { return []Format{FormatCSV} }
func (r guardedRenderer) Render(context.Context, Job, ProgressFunc) (Artifact, error) {
	return Artifact{}, nil
}
func (r guardedRenderer) Policy(Job) (Policy, error) { return r.policy, nil }
func (r guardedRenderer) Internal() bool             { return r.internal }

type plainRenderer struct{}

func (plainRenderer) Kind() Kind        { return KindAttendanceOverview }
func (plainRenderer) Formats() []Format { return []Format{FormatPDF} }
func (plainRenderer) Render(context.Context, Job, ProgressFunc) (Artifact, error) {
	return Artifact{}, nil
}

var leadsRead = workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}

func TestPolicyOfFailsClosedWithoutAGuard(t *testing.T) {
	if _, err := PolicyOf(plainRenderer{}, Job{}); !errors.Is(err, ErrNoPolicy) {
		t.Fatalf("a renderer without a policy = %v", err)
	}
	policy, err := PolicyOf(guardedRenderer{policy: Policy{Required: []workspace.PermissionEntry{leadsRead}, Tier: "basic"}}, Job{})
	if err != nil || policy.Tier != "basic" || len(policy.Required) != 1 {
		t.Fatalf("PolicyOf = %+v, %v", policy, err)
	}
}

func TestTheFormatRouterForwardsThePolicyAndTheInternalMark(t *testing.T) {
	guarded := guardedRenderer{kind: KindLeads, policy: Policy{Required: []workspace.PermissionEntry{leadsRead}}, internal: true}
	router := NewFormatRouter(KindLeads, guarded, plainRenderer{})
	if _, err := PolicyOf(router, Job{Format: FormatPDF}); err != nil {
		t.Fatalf("the router lost the policy: %v", err)
	}
	if !IsInternal(router) {
		t.Fatal("the router lost the internal mark")
	}
	if IsInternal(NewFormatRouter(KindAttendanceOverview, guardedRenderer{kind: KindAttendanceOverview})) {
		t.Fatal("a public kind became internal")
	}
	if _, err := PolicyOf(NewFormatRouter(KindAttendanceOverview, plainRenderer{}), Job{}); !errors.Is(err, ErrNoPolicy) {
		t.Fatalf("a router without a guarded renderer = %v", err)
	}
}

func TestPolicyAllows(t *testing.T) {
	export := workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionExport}
	p := Policy{Required: []workspace.PermissionEntry{leadsRead, export}}
	if !p.Allows(func(workspace.PermissionEntry) bool { return true }) {
		t.Fatal("a holder of every permission is allowed")
	}
	if p.Allows(func(e workspace.PermissionEntry) bool { return e == leadsRead }) {
		t.Fatal("a holder of part of the permissions is allowed")
	}
	if (Policy{}).Allows(func(workspace.PermissionEntry) bool { return true }) {
		t.Fatal("a policy that requires nothing must not open a report")
	}
}

func TestJobReadableByItsRequesterOrHoldersOfThePolicy(t *testing.T) {
	job := Job{RequestedBy: "manager"}
	p := Policy{Required: []workspace.PermissionEntry{leadsRead}}
	none := func(workspace.PermissionEntry) bool { return false }
	all := func(workspace.PermissionEntry) bool { return true }
	if !job.ReadableBy("manager", p, none) {
		t.Fatal("the requester reads its report")
	}
	if job.ReadableBy("finance", p, none) {
		t.Fatal("a member without the permissions reads someone else's report")
	}
	if !job.ReadableBy("other-manager", p, all) {
		t.Fatal("a holder of the same permissions reads the report")
	}
	if job.ReadableBy("", p, none) {
		t.Fatal("nobody reads as an empty user")
	}
}

func TestFingerprintSeparatesRequestersAndTiers(t *testing.T) {
	params := json.RawMessage(`{"a":1}`)
	base := Fingerprint("ws", "u-1", "basic", KindLeads, FormatCSV, "pt", params)
	if Fingerprint("ws", "u-2", "basic", KindLeads, FormatCSV, "pt", params) == base {
		t.Fatal("two requesters share a job")
	}
	if Fingerprint("ws", "u-1", "sensitive", KindLeads, FormatCSV, "pt", params) == base {
		t.Fatal("two tiers share a job")
	}
	if Fingerprint("ws", "u-1", "basic", KindLeads, FormatCSV, "pt", json.RawMessage(`{ "a" : 1 }`)) != base {
		t.Fatal("the same request must reuse the job")
	}
}

func TestWriteCSVRowEndsTheLineAndNeutralisesFormulas(t *testing.T) {
	var b strings.Builder
	if err := WriteCSVRow(&b, []CSVCell{Text("Ana"), Text("=1+1"), Empty(), Text("a;b")}); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "Ana;'=1+1;;\"a;b\""+CSVNewline {
		t.Fatalf("row = %q", got)
	}
}

func TestARequesterOnlyJobIsReadByItsRequesterAlone(t *testing.T) {
	job := Job{RequestedBy: "owner"}
	p := Policy{Required: []workspace.PermissionEntry{leadsRead}, RequesterOnly: true}
	all := func(workspace.PermissionEntry) bool { return true }
	if !job.ReadableBy("owner", p, all) {
		t.Fatal("the requester reads its scoped report")
	}
	if job.ReadableBy("attendant", p, all) {
		t.Fatal("a holder of the same permission read a report rendered with someone else's scope")
	}
}

func TestPolicyReadersAreThePermissionKeysOfASharedPolicy(t *testing.T) {
	export := workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionExport}
	cases := []struct {
		name   string
		policy Policy
		want   []string
	}{
		{"shared, sorted and unique", Policy{Required: []workspace.PermissionEntry{export, leadsRead, export}}, []string{"leads:export", "leads:read"}},
		{"requester only", Policy{Required: []workspace.PermissionEntry{leadsRead}, RequesterOnly: true}, nil},
		{"nothing required", Policy{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.policy.Readers()
			if strings.Join(got, ",") != strings.Join(tc.want, ",") || (tc.want == nil && got != nil) {
				t.Fatalf("readers = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReadersAllowOnlyAViewerHoldingEveryKey(t *testing.T) {
	cases := []struct {
		name    string
		readers []string
		held    []string
		want    bool
	}{
		{"every key held", []string{"leads:export", "leads:read"}, []string{"leads:read", "leads:export", "balance:read"}, true},
		{"one key missing", []string{"leads:export", "leads:read"}, []string{"leads:read"}, false},
		{"requester only", nil, []string{"leads:read"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadersHeld(tc.readers, tc.held); got != tc.want {
				t.Fatalf("ReadersHeld = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReportErrorCodes(t *testing.T) {
	cases := map[error]string{
		ErrNotAllowed:          "report_forbidden",
		ErrKindNotOffered:      "report_kind_not_offered",
		errors.New("anything"): "",
	}
	for err, want := range cases {
		if got := ErrorCode(err); got != want {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
}
