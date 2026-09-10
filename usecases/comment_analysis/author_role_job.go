package comment_analysis_usecase

import (
	"context"
	"log"
	"strings"
	"time"

	"vozko/domain/balance"
	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
)

// The author pass (§5): who is this person, from their own words.
//
// A separate job rather than a step in the classifier, because it works on a
// different unit. The comment pass reads one comment; this reads a PERSON, once
// their corpus is big enough to be worth reading and again only when it has
// grown enough to be worth re-reading. Attaching it to each comment would
// re-bill somebody's entire history every time they typed a full stop.
//
// Three ceilings keep it from becoming a surprise on an invoice, and all three
// are here rather than in the adapter so a test can see them:
//
//   - MinCommentsForRole: nobody is judged on a handful of comments.
//   - ShouldInferRole: a corpus must have doubled since the last look.
//   - MaxAuthorsPerRun: a cycle reads at most this many people, biggest corpus
//     first, so a workspace that just backfilled 50.000 comments does not turn
//     into 8.000 model calls in one tick.

const (
	// MaxAuthorsPerRun caps one account's pass. Deliberately small: this is a
	// background nicety, and the next cycle picks up where it left off.
	MaxAuthorsPerRun = 20
	// MaxCorpusComments caps how much of one person we send. Their most recent
	// comments say more about who they are now than their oldest ones.
	MaxCorpusComments = 40
)

// RoleInferenceDeps groups the collaborators. Every one is checked at call
// time: a deployment without a model simply never infers a role.
type RoleInferenceDeps struct {
	Authors  ca.AuthorRepository
	Repo     ca.Repository
	Settings ca.SettingsRepository
	Adapters map[ca.Source]ca.SourceAdapter
	Inferrer ca.RoleInferrer
	Batches  ca.BatchRepository
	Charger  ca.Charger
	Balance  balance.CachedBalanceChecker
	Clock    ca.Clock
}

// RoleInferenceJob runs the pass for one account at a time.
type RoleInferenceJob struct {
	RoleInferenceDeps
	guard balanceGuard
}

func NewRoleInferenceJob(d RoleInferenceDeps) *RoleInferenceJob {
	return &RoleInferenceJob{RoleInferenceDeps: d, guard: newBalanceGuard(d.Balance, "role pass")}
}

// RunAccount infers roles for one account's authors. Best effort throughout:
// this is context for a human, so one author the model could not read must not
// stop the rest, and nothing here may fail the rollup that called it.
func (j *RoleInferenceJob) RunAccount(ctx context.Context, source ca.Source, accountID string) (inferred int) {
	if j.Inferrer == nil || j.Authors == nil || j.Repo == nil {
		return 0
	}

	candidates, err := j.Authors.ListForRoleInference(ctx, source, accountID, ca.MinCommentsForRole, MaxAuthorsPerRun*4)
	if err != nil {
		log.Printf("[comment-analysis] role pass: listing authors of %s: %v", accountID, err)
		return 0
	}

	settings := j.settingsFor(ctx, source, accountID)
	for _, author := range candidates {
		if inferred >= MaxAuthorsPerRun {
			return inferred
		}
		if author == nil || !ca.ShouldInferRole(author.Role, author.Total) {
			continue
		}
		// Re-checked per author rather than once for the account: a pass of
		// twenty calls can cross the floor part way through.
		if err := j.guard.Allow(author.WorkspaceID); err != nil {
			return inferred
		}
		if j.inferOne(ctx, author, settings) {
			inferred++
		}
	}
	return inferred
}

func (j *RoleInferenceJob) settingsFor(ctx context.Context, source ca.Source, accountID string) *ca.Settings {
	if j.Settings != nil {
		if s, err := j.Settings.Find(ctx, source, accountID); err == nil && s != nil {
			return s
		}
	}
	return nil
}

// inferOne reads one person and stores what came back. It reports whether a
// model call was actually made, which is what the caller's cap counts.
func (j *RoleInferenceJob) inferOne(ctx context.Context, author *ca.AuthorStats, settings *ca.Settings) bool {
	corpus := j.corpus(ctx, author)
	// The corpus can come back short even when the counters say otherwise: the
	// channel may have lost comments to deletion since the rollup counted them.
	if len(corpus) < ca.MinCommentsForRole {
		return false
	}

	req := ca.RoleInferRequest{
		WorkspaceID: author.WorkspaceID,
		Comments:    corpus,
	}
	if settings != nil {
		req.Model, req.Instructions = settings.Model, settings.Instructions
	}

	result, err := j.Inferrer.InferRole(ctx, req)
	if err != nil {
		log.Printf("[comment-analysis] role pass: author %s: %v", author.ID, err)
		return false
	}

	j.record(ctx, author, result, len(corpus))

	inference := ca.AuthorRoleInference{
		Role:            result.Role,
		Confidence:      shared.QualityLevel(result.Confidence),
		BasedOnComments: len(corpus),
		Rationale:       result.Rationale,
	}
	inference.Normalize()
	if err := j.Authors.SetRole(ctx, author.WorkspaceID, author.ID, inference, j.now()); err != nil {
		log.Printf("[comment-analysis] role pass: saving author %s: %v", author.ID, err)
	}
	// The call happened and was paid for either way, so it counts against the
	// cap even when the answer was "unknown" or the write failed.
	return true
}

// corpus reads the person's own words back from the channel, most recent
// first. The engine stores excerpts; the excerpt is the fallback when the
// channel cannot answer, because a shorter corpus is better than none.
func (j *RoleInferenceJob) corpus(ctx context.Context, author *ca.AuthorStats) []string {
	page, err := j.Repo.List(ctx, ca.ListInput{
		WorkspaceID:      author.WorkspaceID,
		Source:           author.Source,
		AccountID:        author.AccountID,
		AuthorExternalID: author.AuthorExternalID,
		Statuses:         []ca.Status{ca.StatusAnalyzed},
		Options:          shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: MaxCorpusComments}},
	})
	if err != nil || page == nil {
		return nil
	}

	byContainer := map[string][]string{}
	order := make([]string, 0, 4)
	excerpts := make(map[string]string, len(page.Items))
	for _, row := range page.Items {
		if row == nil {
			continue
		}
		if _, seen := byContainer[row.ContainerID]; !seen {
			order = append(order, row.ContainerID)
		}
		byContainer[row.ContainerID] = append(byContainer[row.ContainerID], row.SourceCommentID)
		excerpts[row.SourceCommentID] = row.Excerpt
	}

	// One read per post, because that is the unit the channel indexes by.
	texts := map[string]string{}
	if adapter, ok := j.Adapters[author.Source]; ok && adapter != nil {
		for _, containerID := range order {
			ref := ca.ContainerRef{Source: author.Source, AccountID: author.AccountID, ContainerID: containerID}
			if got, err := adapter.ReadTexts(ctx, ref, byContainer[containerID]); err == nil {
				for id, text := range got {
					texts[id] = text
				}
			}
		}
	}

	out := make([]string, 0, len(page.Items))
	for _, row := range page.Items {
		if row == nil {
			continue
		}
		text := strings.TrimSpace(texts[row.SourceCommentID])
		if text == "" {
			text = strings.TrimSpace(excerpts[row.SourceCommentID])
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

// record books the call the way the comment pass books its own, tagged as the
// author pass so /spend can show the two separately.
func (j *RoleInferenceJob) record(ctx context.Context, author *ca.AuthorStats, result *ca.RoleInferResult, items int) {
	if j.Batches == nil {
		return
	}
	// Built and written by the shared helpers; the charge in between is what
	// makes this path two steps rather than one call to recordAICall.
	batch := newAIBatch(j.Clock, aiCall{
		WorkspaceID: author.WorkspaceID,
		Source:      author.Source,
		AccountID:   author.AccountID,
		Kind:        ca.BatchKindAuthorRole,
		Model:       result.Model,
		ItemCount:   items,

		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,
	})
	if j.Charger != nil {
		// Idempotent on the batch id, exactly as the comment pass charges.
		if micros, err := j.Charger.ChargeBatch(ctx, batch.WorkspaceID, batch.ID, items); err == nil {
			batch.PriceMicros = micros
		}
	}
	writeBatch(ctx, j.Batches, batch)
}

func (j *RoleInferenceJob) now() time.Time {
	if j.Clock != nil {
		return j.Clock.Now()
	}
	return time.Now().UTC()
}
