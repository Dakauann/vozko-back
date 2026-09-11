package audience

import (
	"testing"
	"time"
)

func analyzedRow(id string, severity int) *Analysis {
	at := time.Now().UTC()
	return &Analysis{
		ID: id, WorkspaceID: "ws-1", Source: SourceInstagram, AccountID: "acc-1", ContainerID: "media-1",
		AuthorExternalID: "ig-" + id, AuthorHandle: "u" + id,
		Status: StatusAnalyzed, Stance: StanceCritic, Severity: severity,
		Excerpt: "texto " + id, AnalyzedAt: &at,
	}
}

// Only analysed rows reach the feed: a pending or failed row has nothing to
// draw, and a feed showing them would show blanks.
func TestBatchAnalyzedSkipsUnanalysedRows(t *testing.T) {
	pending := analyzedRow("p", 0)
	pending.Status = StatusPending
	failed := analyzedRow("f", 0)
	failed.Status = StatusFailed

	out := NewAnalysisBatchAnalyzed([]*Analysis{pending, failed, analyzedRow("a", 10), nil})
	if out == nil {
		t.Fatal("one analysed row must produce a broadcast")
	}
	if len(out.Items) != 1 || out.Items[0].CommentID != "a" {
		t.Fatalf("items = %+v", out.Items)
	}
	if out.WorkspaceID != "ws-1" || out.ContainerID != "media-1" {
		t.Fatalf("scope = %+v", out)
	}
}

// A batch with nothing to show produces nothing to send, so a caller can just
// check for nil rather than for an empty slice it must not broadcast.
func TestBatchAnalyzedIsNilWhenThereIsNothingToShow(t *testing.T) {
	pending := analyzedRow("p", 0)
	pending.Status = StatusPending
	if out := NewAnalysisBatchAnalyzed([]*Analysis{pending}); out != nil {
		t.Fatalf("out = %+v, want nil", out)
	}
	if out := NewAnalysisBatchAnalyzed(nil); out != nil {
		t.Fatalf("out = %+v, want nil", out)
	}
}

// THE backpressure test. A backfill batch must not turn into an unbounded
// message, and what survives the cap must be the worst rows, not the first
// ones: truncating by arrival order would reliably drop the row that mattered.
func TestBatchAnalyzedCapsAndKeepsTheWorst(t *testing.T) {
	rows := make([]*Analysis, 0, 200)
	for i := 0; i < 200; i++ {
		// Ascending severity, so arrival order is the WORST possible order to
		// truncate by.
		rows = append(rows, analyzedRow(string(rune('a'+i%26))+string(rune('0'+i/26)), i))
	}

	out := NewAnalysisBatchAnalyzed(rows)
	if out == nil {
		t.Fatal("nil")
	}
	if len(out.Items) != MaxLiveEvents {
		t.Fatalf("items = %d, want the cap %d", len(out.Items), MaxLiveEvents)
	}
	if out.More != 200-MaxLiveEvents {
		t.Fatalf("more = %d, want %d", out.More, 200-MaxLiveEvents)
	}
	if out.Items[0].Severity != 199 {
		t.Fatalf("first item severity = %d, the worst row must survive", out.Items[0].Severity)
	}
	for i := 1; i < len(out.Items); i++ {
		if out.Items[i].Severity > out.Items[i-1].Severity {
			t.Fatalf("not ordered worst first at %d: %+v", i, out.Items)
		}
	}
}

// Under the cap nothing is claimed to be omitted.
func TestBatchAnalyzedReportsNoOverflowWhenItFits(t *testing.T) {
	out := NewAnalysisBatchAnalyzed([]*Analysis{analyzedRow("a", 5), analyzedRow("b", 90)})
	if out.More != 0 {
		t.Fatalf("more = %d, want 0", out.More)
	}
	if out.Items[0].Severity != 90 {
		t.Fatalf("worst first: %+v", out.Items)
	}
}

// The event carries the excerpt and never a full comment body: the engine does
// not store one, and the feed must not imply it does.
func TestCommentAnalyzedCarriesOnlyWhatIsStored(t *testing.T) {
	row := analyzedRow("a", 42)
	e := NewCommentAnalyzed(row)
	if e.Excerpt != row.Excerpt || e.Severity != 42 || e.AuthorHandle != "ua" {
		t.Fatalf("event = %+v", e)
	}
	if e.AnalyzedAt.IsZero() {
		t.Fatal("an analysed row has an analysed timestamp")
	}
}
