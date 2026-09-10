package comment_analysis_usecase

import (
	"context"
	"errors"
	"testing"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
)

type fakeInferrer struct {
	calls   int
	seen    []ca.RoleInferRequest
	result  *ca.RoleInferResult
	err     error
	corpora [][]string
}

func (f *fakeInferrer) InferRole(_ context.Context, req ca.RoleInferRequest) (*ca.RoleInferResult, error) {
	f.calls++
	f.seen = append(f.seen, req)
	f.corpora = append(f.corpora, req.Comments)
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &ca.RoleInferResult{Role: ca.RolePolitician, Confidence: string(shared.QualityLevelHigh), Model: "m"}, nil
}

// roleFixture seeds one author with `comments` analysed comments, so the pass
// has a corpus to read.
func roleFixture(t *testing.T, comments int, previous ca.AuthorRoleInference) (*RoleInferenceJob, *fakeAuthors, *fakeInferrer, *fakeBatches) {
	t.Helper()
	repo := newFakeRepo()
	for i := 0; i < comments; i++ {
		a, err := ca.NewPending(ca.NewInput{
			WorkspaceID: "ws-1", Container: ref(), SourceCommentID: "c-" + itoa(i),
			AuthorExternalID: "ig-99", AuthorHandle: "fulano",
			Text: "como vereador eu cobrei isso na câmara " + itoa(i), Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		a.ID = "row-" + itoa(i)
		if _, err := repo.Insert(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		_ = a.Claim(now)
		if err := a.Apply(ca.Classification{
			Sentiment: shared.SentimentNeutral, Stance: ca.StanceNeutral, Intent: ca.IntentOther,
			TopicKey: "other", Language: "pt",
			Toxicity: shared.QualityLevelNone, PersonalAttack: shared.QualityLevelNone, LegalRisk: shared.QualityLevelNone,
		}, ca.ActionPolicy{}, ca.Provenance{BatchID: "b", Model: "m"}, now); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}

	author := &ca.AuthorStats{
		ID: "a-1", WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: ref().AccountID,
		AuthorExternalID: "ig-99", AuthorHandle: "fulano", Role: previous,
	}
	author.Total = comments
	authors := &fakeAuthors{rows: []*ca.AuthorStats{author}}
	inferrer := &fakeInferrer{}
	batches := &fakeBatches{}

	job := NewRoleInferenceJob(RoleInferenceDeps{
		Authors: authors, Repo: repo, Inferrer: inferrer, Batches: batches, Clock: fixedClock{now},
	})
	return job, authors, inferrer, batches
}

// The happy path: a big enough corpus is read once and the answer is stored.
func TestRolePassInfersAndStores(t *testing.T) {
	job, authors, inferrer, batches := roleFixture(t, 20, ca.AuthorRoleInference{})

	if got := job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID); got != 1 {
		t.Fatalf("inferred = %d, want 1", got)
	}
	if inferrer.calls != 1 {
		t.Fatalf("model calls = %d", inferrer.calls)
	}
	stored := authors.rows[0].Role
	if stored.Role != ca.RolePolitician || stored.BasedOnComments != 20 {
		t.Fatalf("stored = %+v", stored)
	}
	if !stored.Displayable() {
		t.Fatal("a high-confidence role over a 20-comment corpus must be displayable")
	}
	// The pass is a new token consumer and has to show up on the invoice under
	// its own name.
	if len(batches.rows) != 1 {
		t.Fatalf("batches = %d, the pass must be billed", len(batches.rows))
	}
	if batches.rows[0].Kind != ca.BatchKindAuthorRole {
		t.Fatalf("batch kind = %q", batches.rows[0].Kind)
	}
}

// Nobody is judged on a handful of comments, and the model is never called for
// them, because the call is what costs money.
func TestRolePassSkipsThinCorpora(t *testing.T) {
	job, _, inferrer, batches := roleFixture(t, ca.MinCommentsForRole-1, ca.AuthorRoleInference{})

	if got := job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID); got != 0 {
		t.Fatalf("inferred = %d, want 0", got)
	}
	if inferrer.calls != 0 {
		t.Fatal("a thin corpus must not reach the model")
	}
	if len(batches.rows) != 0 {
		t.Fatal("nothing to bill")
	}
}

// THE cost test. An author already inferred whose corpus barely grew must not
// be re-read: otherwise every new comment re-bills their whole history.
func TestRolePassDoesNotRebillAnUnchangedCorpus(t *testing.T) {
	previous := ca.AuthorRoleInference{
		Role: ca.RolePolitician, Confidence: shared.QualityLevelHigh, BasedOnComments: 20,
	}
	job, _, inferrer, _ := roleFixture(t, 21, previous)

	if got := job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID); got != 0 {
		t.Fatalf("inferred = %d, want 0", got)
	}
	if inferrer.calls != 0 {
		t.Fatalf("model calls = %d: one more comment must not re-read the corpus", inferrer.calls)
	}
}

// A corpus that doubled is worth another look.
func TestRolePassRerunsOnAGrownCorpus(t *testing.T) {
	previous := ca.AuthorRoleInference{
		Role: ca.RoleUnknown, Confidence: shared.QualityLevelNone, BasedOnComments: 12,
	}
	job, _, inferrer, _ := roleFixture(t, 30, previous)

	if got := job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID); got != 1 {
		t.Fatalf("inferred = %d, want 1", got)
	}
	if inferrer.calls != 1 {
		t.Fatalf("model calls = %d", inferrer.calls)
	}
}

// A model that cannot answer costs the customer a failed call and nothing
// else: the author keeps whatever they had, and the pass moves on.
func TestRolePassSurvivesAModelError(t *testing.T) {
	job, authors, inferrer, batches := roleFixture(t, 20, ca.AuthorRoleInference{})
	inferrer.err = errors.New("model down")

	if got := job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID); got != 0 {
		t.Fatalf("inferred = %d", got)
	}
	if authors.rows[0].Role.Role != "" && authors.rows[0].Role.Role != ca.RoleUnknown {
		t.Fatalf("role = %q, a failed call must not write one", authors.rows[0].Role.Role)
	}
	if len(batches.rows) != 0 {
		t.Fatal("a call that returned nothing is not a batch to bill")
	}
}

// A label outside the taxonomy is discarded on the way in, not stored and
// filtered on the way out.
func TestRolePassDiscardsAnInventedLabel(t *testing.T) {
	job, authors, inferrer, _ := roleFixture(t, 20, ca.AuthorRoleInference{})
	inferrer.result = &ca.RoleInferResult{Role: "influencer", Confidence: string(shared.QualityLevelHigh), Model: "m"}

	job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID)
	if authors.rows[0].Role.Role != ca.RoleUnknown {
		t.Fatalf("role = %q, want unknown", authors.rows[0].Role.Role)
	}
	if authors.rows[0].Role.Displayable() {
		t.Fatal("an invented label must never be displayable")
	}
}

// A deployment with no model configured runs the rollup exactly as before.
func TestRolePassWithoutAnInferrerDoesNothing(t *testing.T) {
	job, _, _, _ := roleFixture(t, 20, ca.AuthorRoleInference{})
	job.Inferrer = nil
	if got := job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID); got != 0 {
		t.Fatalf("inferred = %d", got)
	}
}

// The model reads the person's WORDS, and nothing that would let it answer
// from a stereotype instead.
func TestRolePassSendsOnlyTheWords(t *testing.T) {
	job, _, inferrer, _ := roleFixture(t, 20, ca.AuthorRoleInference{})
	job.RunAccount(context.Background(), ca.SourceInstagram, ref().AccountID)

	if len(inferrer.seen) != 1 {
		t.Fatalf("calls = %d", len(inferrer.seen))
	}
	req := inferrer.seen[0]
	if req.WorkspaceID != "ws-1" {
		t.Fatalf("workspace = %q, the call must be billed to somebody", req.WorkspaceID)
	}
	if len(req.Comments) < ca.MinCommentsForRole {
		t.Fatalf("corpus = %d comments", len(req.Comments))
	}
	if len(req.Comments) > MaxCorpusComments {
		t.Fatalf("corpus = %d comments, over the cap", len(req.Comments))
	}
}
