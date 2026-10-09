package leadimport

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/lead"
)

var now = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

func uploaded(t *testing.T) *Job {
	t.Helper()
	j, err := NewJob("ws-1", "user-1", File{MediaID: "m-1", Name: "leads.csv", SizeBytes: 1200, Owned: true}, Preview{Headers: []string{"numero"}}, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestNewJobHoldsTheCaps(t *testing.T) {
	j := uploaded(t)
	if j.Status != StatusUploaded || !j.ExpiresAt.Equal(now.Add(Retention)) || j.TotalRows != 10 || j.WorkspaceID != "ws-1" || j.RequestedBy != "user-1" {
		t.Fatalf("job = %+v", j)
	}
	cases := []struct {
		name string
		file File
		rows int
		want error
	}{
		{"a file above 20 MB", File{MediaID: "m", SizeBytes: MaxFileBytes + 1}, 10, ErrFileTooLarge},
		{"more rows than the cap", File{MediaID: "m", SizeBytes: 10}, MaxRows + 1, ErrTooManyRows},
		{"a header without rows", File{MediaID: "m", SizeBytes: 10}, 0, ErrFileEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewJob("ws-1", "user-1", tc.file, Preview{Headers: []string{"numero"}}, tc.rows, now); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := NewJob("", "user-1", File{MediaID: "m"}, Preview{}, 1, now); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewJob("ws-1", "", File{MediaID: "m"}, Preview{}, 1, now); !errors.Is(err, ErrRequesterRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestOnlyTheImporterSeesTheJob(t *testing.T) {
	j := uploaded(t)
	if !j.VisibleTo("user-1") || j.VisibleTo("user-2") || j.VisibleTo("") {
		t.Fatal("an import is visible to the person who uploaded it and nobody else")
	}
}

func TestConfigureAndStartFollowTheLifecycle(t *testing.T) {
	settings := settingsWith(FieldNumber)
	j := uploaded(t)
	if err := j.Start(Grants{}, now); !errors.Is(err, ErrNotReady) {
		t.Fatalf("start before a dry run = %v, want ErrNotReady", err)
	}
	if err := j.Configure(settings, now); err != nil {
		t.Fatal(err)
	}
	if j.Status != StatusAnalyzing || j.Fingerprint != settings.Fingerprint() || j.DryRun != nil || j.Attempts != 0 || j.Claim != "" {
		t.Fatalf("configured job = %+v", j)
	}
	if err := j.Start(Grants{}, now); !errors.Is(err, ErrNotReady) {
		t.Fatalf("start during the dry run = %v, want ErrNotReady", err)
	}
	j.Status, j.DryRun = StatusAnalyzed, &Counts{Rows: 10}
	if err := j.Configure(settingsWith(FieldName), now); err != nil {
		t.Fatalf("a new dry run after the first one = %v", err)
	}
	j.Status, j.DryRun = StatusAnalyzed, &Counts{Rows: 10}
	if err := j.Start(Grants{}, now); err != nil {
		t.Fatal(err)
	}
	if j.Status != StatusImporting || j.Stage != StageRows || j.Processed != 0 || j.Result == nil || j.StartedAt == nil {
		t.Fatalf("started job = %+v", j)
	}
	if !j.Result.AddressesMeasured() || *j.Result.NoAddress != 0 {
		t.Fatalf("a started import measures where its addresses go: %+v", j.Result)
	}
	for _, status := range []Status{StatusImporting, StatusDone} {
		j.Status = status
		if err := j.Configure(settings, now); !errors.Is(err, ErrNotReady) {
			t.Fatalf("configure while %s = %v, want ErrNotReady", status, err)
		}
	}
}

func TestClaimability(t *testing.T) {
	stale := now.Add(-StaleAfter - time.Second)
	fresh := now.Add(-time.Second)
	cases := []struct {
		name string
		job  Job
		want bool
	}{
		{"queued and never claimed", Job{Status: StatusImporting}, true},
		{"claimed and alive", Job{Status: StatusImporting, Claim: "c", HeartbeatAt: &fresh, Attempts: 1}, false},
		{"claimed and silent", Job{Status: StatusAnalyzing, Claim: "c", HeartbeatAt: &stale, Attempts: 1}, true},
		{"out of attempts", Job{Status: StatusImporting, Claim: "c", HeartbeatAt: &stale, Attempts: MaxAttempts}, false},
		{"done", Job{Status: StatusDone}, false},
		{"waiting for the person", Job{Status: StatusAnalyzed}, false},
	}
	for _, tc := range cases {
		if got := tc.job.Claimable(now); got != tc.want {
			t.Errorf("%s: Claimable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCountsRecordEachDecision(t *testing.T) {
	var c Counts
	c.Record(lead.ImportDecision{Verdict: lead.ImportCreated, Lead: &lead.Lead{Addresses: []lead.Address{{Fix: nil}}}}, nil)
	c.Record(lead.ImportDecision{Verdict: lead.ImportEnriched, Conflicts: []string{"name"}, NewAddress: &lead.Address{GeoStatus: lead.GeoLocated}}, &lead.Lead{Blocked: true})
	c.Record(lead.ImportDecision{Verdict: lead.ImportUnchanged}, &lead.Lead{})
	c.Record(lead.ImportDecision{Verdict: lead.ImportSkipped}, &lead.Lead{Blocked: true})
	c.Record(lead.ImportDecision{Verdict: lead.ImportRejected, Issues: []lead.ImportIssue{{Reason: lead.ReasonLeadChanging, Rejected: true}}}, nil)
	c.Issue(lead.ImportIssue{Reason: lead.ReasonDuplicate, Rejected: true})
	c.Issue(lead.ImportIssue{Reason: lead.ReasonEmailInvalid})
	want := Counts{Rows: 6, Created: 1, Enriched: 1, Unchanged: 1, Skipped: 1, Rejected: 2, Conflicting: 1, Blocked: 2, AddressesAdded: 2, AddressesLocated: 1,
		Issues: map[string]int{string(lead.ReasonLeadChanging): 1, string(lead.ReasonDuplicate): 1, string(lead.ReasonEmailInvalid): 1}}
	if c.Rows != want.Rows || c.Created != want.Created || c.Enriched != want.Enriched || c.Unchanged != want.Unchanged || c.Skipped != want.Skipped ||
		c.Rejected != want.Rejected || c.Conflicting != want.Conflicting || c.Blocked != want.Blocked || c.AddressesAdded != want.AddressesAdded ||
		c.AddressesLocated != want.AddressesLocated || len(c.Issues) != 3 || c.Issues["duplicate"] != 1 {
		t.Fatalf("counts = %+v, want %+v", c, want)
	}
}

func TestCountsRecordRowCountsTheRowsThatBroughtNoAddress(t *testing.T) {
	home := &lead.Address{GeoStatus: lead.GeoPending}
	c := NewCounts()
	c.RecordRow(lead.ImportRecord{}, lead.ImportDecision{Verdict: lead.ImportCreated, Lead: &lead.Lead{}}, nil)
	c.RecordRow(lead.ImportRecord{Address: home, AddressGiven: true}, lead.ImportDecision{Verdict: lead.ImportCreated, Lead: &lead.Lead{Addresses: []lead.Address{*home}}}, nil)
	c.RecordRow(lead.ImportRecord{AddressGiven: true}, lead.ImportDecision{Verdict: lead.ImportCreated, Lead: &lead.Lead{}, Issues: []lead.ImportIssue{{Reason: lead.ReasonAddressInvalid}}}, nil)
	c.RecordRow(lead.ImportRecord{}, lead.ImportDecision{Verdict: lead.ImportSkipped}, &lead.Lead{})
	c.RecordRow(lead.ImportRecord{}, lead.ImportDecision{Verdict: lead.ImportRejected, Issues: []lead.ImportIssue{{Reason: lead.ReasonLeadChanging, Rejected: true}}}, nil)
	if c.Rows != 5 || c.Created != 3 || c.Skipped != 1 || c.Rejected != 1 || c.AddressesAdded != 1 || !c.AddressesMeasured() || *c.NoAddress != 2 {
		t.Fatalf("counts = %+v, want two rows without an address, the invalid address and the rejected row left out", c)
	}
}

func TestCountsFromBeforeTheAddressCountStayUnmeasured(t *testing.T) {
	var legacy Counts
	legacy.RecordRow(lead.ImportRecord{}, lead.ImportDecision{Verdict: lead.ImportCreated, Lead: &lead.Lead{}}, nil)
	if legacy.AddressesMeasured() || legacy.NoAddress != nil || legacy.Created != 1 {
		t.Fatalf("legacy counts = %+v, want the address count left unknown", legacy)
	}
}

func TestCloneDoesNotShareTheAddressCount(t *testing.T) {
	c := NewCounts()
	c.RecordRow(lead.ImportRecord{}, lead.ImportDecision{Verdict: lead.ImportCreated, Lead: &lead.Lead{}}, nil)
	clone := c.Clone()
	clone.RecordRow(lead.ImportRecord{}, lead.ImportDecision{Verdict: lead.ImportCreated, Lead: &lead.Lead{}}, nil)
	if *c.NoAddress != 1 || *clone.NoAddress != 2 {
		t.Fatalf("original = %d, clone = %d", *c.NoAddress, *clone.NoAddress)
	}
}

func TestCountsRecordTheAddressesAnImportCompleted(t *testing.T) {
	c := NewCounts()
	filled := &lead.Address{ID: "addr-1", GeoStatus: lead.GeoPending}
	c.RecordRow(lead.ImportRecord{AddressGiven: true, Address: filled}, lead.ImportDecision{Verdict: lead.ImportEnriched, FilledAddress: filled}, &lead.Lead{})
	c.RecordRow(lead.ImportRecord{AddressGiven: true, Address: filled}, lead.ImportDecision{Verdict: lead.ImportUnchanged}, &lead.Lead{})
	if c.AddressesFilled != 1 || c.AddressesAdded != 0 || *c.NoAddress != 0 {
		t.Fatalf("counts = %+v, want one completed address and no row counted without an address", c)
	}
}

func TestLimitsAreTheDomainCaps(t *testing.T) {
	got := CurrentLimits()
	want := Limits{MaxBytes: MaxFileBytes, MaxMegabytes: MaxFileBytes >> 20, MaxRows: MaxRows, MaxSeededConversations: 200, RetentionDays: 7, MaxUnusedUploads: MaxUnusedUploads}
	if got != want {
		t.Fatalf("limits = %+v, want %+v", got, want)
	}
}

func TestListedKeepsTheImportersLiveImportsNewestFirst(t *testing.T) {
	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes) * time.Minute) }
	jobs := []Job{
		{ID: "old", RequestedBy: "user-1", CreatedAt: at(-10), ExpiresAt: at(100)},
		{ID: "other", RequestedBy: "user-2", CreatedAt: at(-5), ExpiresAt: at(100)},
		{ID: "expired", RequestedBy: "user-1", CreatedAt: at(-20), ExpiresAt: at(-1)},
		{ID: "new", RequestedBy: "user-1", CreatedAt: at(-1), ExpiresAt: at(100)},
	}
	got := Listed(jobs, "user-1", now)
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("listed = %+v", got)
	}
	if Listed(jobs, " ", now) != nil {
		t.Fatal("nobody lists the imports of nobody")
	}
	many := make([]Job, MaxListed+3)
	for i := range many {
		many[i] = Job{ID: fmt.Sprintf("imp-%d", i), RequestedBy: "user-1", CreatedAt: at(i), ExpiresAt: at(1000)}
	}
	if listed := Listed(many, "user-1", now); len(listed) != MaxListed || listed[0].ID != fmt.Sprintf("imp-%d", MaxListed+2) {
		t.Fatalf("listed %d, first %s", len(listed), listed[0].ID)
	}
}

func TestEveryReasonHasALabelInEveryLocale(t *testing.T) {
	for _, reason := range Reasons() {
		for _, locale := range []string{"pt", "en", "es", "de"} {
			if label := ReasonLabel(reason, locale); label == "" || label == string(reason) {
				t.Errorf("reason %q has no %s label", reason, locale)
			}
		}
	}
	if ReasonLabel(lead.ReasonDuplicate, "fr") != ReasonLabel(lead.ReasonDuplicate, "pt") {
		t.Error("an unknown locale falls back to Portuguese")
	}
}

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrNotFound, "lead_import_not_found"},
		{ErrRunning, "lead_import_running"},
		{ErrNotReady, "lead_import_not_ready"},
		{ErrFileTooLarge, "lead_import_file_too_large"},
		{ErrTooManyRows, "lead_import_too_many_rows"},
		{ErrFileEmpty, "lead_import_empty"},
		{ErrUnsupportedFile, "lead_import_unsupported_file"},
		{ErrFileUnavailable, "lead_import_file_unavailable"},
		{ErrUnavailable, "lead_import_unavailable"},
		{&MappingError{Column: 2, Field: "cpf", Rule: RuleUnknownField}, "lead_import_mapping_invalid"},
		{&PermissionError{Action: "assign"}, "lead_import_forbidden"},
		{ErrOnlyMine, "lead_import_only_mine"},
		{errors.New("other"), ""},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
