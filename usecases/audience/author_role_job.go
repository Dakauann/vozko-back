package audience_usecase

import (
	"context"
	"log"
	"strings"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/balance"
	"vozko/domain/shared"
)

const (
	MaxAuthorsPerRun  = 20
	MaxCorpusComments = 40
)

type RoleInferenceDeps struct {
	Authors  ca.AuthorRepository
	Repo     ca.Repository
	Settings ca.SettingsRepository
	Adapters map[ca.Source]ca.SourceAdapter
	Inferrer ca.RoleInferrer
	Batches  ca.BatchRepository
	Balance  balance.CachedBalanceChecker
	Clock    ca.Clock
}

type RoleInferenceJob struct {
	RoleInferenceDeps
	guard balanceGuard
}

func NewRoleInferenceJob(d RoleInferenceDeps) *RoleInferenceJob {
	return &RoleInferenceJob{RoleInferenceDeps: d, guard: newBalanceGuard(d.Balance, "role pass")}
}

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

func (j *RoleInferenceJob) inferOne(ctx context.Context, author *ca.AuthorStats, settings *ca.Settings) bool {
	corpus := j.corpus(ctx, author)
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
	return true
}

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
		byContainer[row.ContainerID] = append(byContainer[row.ContainerID], row.SubjectID)
		excerpts[row.SubjectID] = row.Excerpt
	}

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
		text := strings.TrimSpace(texts[row.SubjectID])
		if text == "" {
			text = strings.TrimSpace(excerpts[row.SubjectID])
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func (j *RoleInferenceJob) record(ctx context.Context, author *ca.AuthorStats, result *ca.RoleInferResult, items int) {
	if j.Batches == nil {
		return
	}
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
	writeBatch(ctx, j.Batches, batch)
}

func (j *RoleInferenceJob) now() time.Time {
	if j.Clock != nil {
		return j.Clock.Now()
	}
	return time.Now().UTC()
}
