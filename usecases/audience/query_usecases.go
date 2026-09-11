package audience_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// Read-side and settings use cases. Every one scopes to the caller's
// workspace inside the use case; the handler only passes it through.

// ---- list / stats / trends ----

type listUseCase struct{ repo ca.Repository }

func NewListUseCase(repo ca.Repository) ca.ListUseCase { return &listUseCase{repo: repo} }

func (uc *listUseCase) Execute(ctx context.Context, in ca.ListInput) (*shared.PaginatedResult[*ca.Analysis], error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}
	return uc.repo.List(ctx, in)
}

type statsUseCase struct{ repo ca.Repository }

func NewStatsUseCase(repo ca.Repository) ca.StatsUseCase { return &statsUseCase{repo: repo} }

func (uc *statsUseCase) Execute(ctx context.Context, in ca.ListInput) (*ca.Stats, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}
	stats, err := uc.repo.GetStats(ctx, in)
	if err != nil {
		return nil, err
	}
	stats.Finalize()
	return stats, nil
}

type trendsUseCase struct {
	repo    ca.TrendRepository
	rollups ca.RollupRepository
}

func NewTrendsUseCase(repo ca.TrendRepository, rollups ca.RollupRepository) ca.TrendsUseCase {
	return &trendsUseCase{repo: repo, rollups: rollups}
}

func (uc *trendsUseCase) ExecuteFiltered(ctx context.Context, in ca.ListInput) ([]*ca.Rollup, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}
	rows, err := uc.repo.GetTrend(ctx, in)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*ca.Rollup{}
	}
	return rows, nil
}

func (uc *trendsUseCase) Execute(ctx context.Context, in ca.TrendInput) ([]*ca.Rollup, error) {
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.ScopeID = strings.TrimSpace(in.ScopeID)
	if err := in.Validate(); err != nil {
		return nil, err
	}
	rows, err := uc.rollups.ListSeries(ctx, in)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*ca.Rollup{}
	}
	return rows, nil
}

// ---- authors ----

type listAuthorsUseCase struct{ authors ca.AuthorRepository }

func NewListAuthorsUseCase(authors ca.AuthorRepository) ca.ListAuthorsUseCase {
	return &listAuthorsUseCase{authors: authors}
}

func (uc *listAuthorsUseCase) Execute(ctx context.Context, in ca.AuthorsInput) (*shared.PaginatedResult[*ca.AuthorStats], error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}
	return uc.authors.List(ctx, in)
}

type getAuthorUseCase struct {
	authors ca.AuthorRepository
	repo    ca.Repository
}

func NewGetAuthorUseCase(authors ca.AuthorRepository, repo ca.Repository) ca.GetAuthorUseCase {
	return &getAuthorUseCase{authors: authors, repo: repo}
}

func (uc *getAuthorUseCase) Execute(ctx context.Context, workspaceID, authorID string, page shared.Pagination) (*ca.AuthorDetail, error) {
	author, err := uc.authors.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(authorID))
	if err != nil {
		return nil, err
	}
	comments, err := uc.repo.List(ctx, ca.ListInput{
		WorkspaceID:      author.WorkspaceID,
		Source:           author.Source,
		AccountID:        author.AccountID,
		AuthorExternalID: author.AuthorExternalID,
		Options:          shared.QueryOptions{Pagination: page},
	})
	if err != nil {
		return nil, err
	}
	return &ca.AuthorDetail{Author: author, Comments: comments}, nil
}

// listAuthorContainersUseCase answers "which posts has this person commented
// on" (§2).
//
// It resolves the author FIRST and then queries with that row's own
// (workspace, source, account, external id). The caller supplies an author id
// and never a scope, so a caller cannot read one workspace's author and then
// ask for another workspace's posts.
type listAuthorContainersUseCase struct {
	authors ca.AuthorRepository
	repo    ca.Repository
}

func NewListAuthorContainersUseCase(authors ca.AuthorRepository, repo ca.Repository) ca.ListAuthorContainersUseCase {
	return &listAuthorContainersUseCase{authors: authors, repo: repo}
}

func (uc *listAuthorContainersUseCase) Execute(ctx context.Context, req ca.AuthorContainersRequest) (*ca.AuthorContainers, error) {
	author, err := uc.authors.FindByID(ctx, strings.TrimSpace(req.WorkspaceID), strings.TrimSpace(req.AuthorID))
	if err != nil {
		return nil, err
	}
	in := ca.AuthorContainersInput{
		WorkspaceID:      author.WorkspaceID,
		Source:           author.Source,
		AccountID:        author.AccountID,
		AuthorExternalID: author.AuthorExternalID,
		From:             req.From,
		To:               req.To,
		Options:          shared.QueryOptions{Pagination: req.Page},
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}
	containers, err := uc.repo.ListAuthorContainers(ctx, in)
	if err != nil {
		return nil, err
	}
	return &ca.AuthorContainers{Author: author, Containers: containers}, nil
}

type setModerationStateUseCase struct {
	authors ca.AuthorRepository
	clock   ca.Clock
}

func NewSetModerationStateUseCase(authors ca.AuthorRepository, clock ca.Clock) ca.SetModerationStateUseCase {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &setModerationStateUseCase{authors: authors, clock: clock}
}

func (uc *setModerationStateUseCase) Execute(ctx context.Context, in ca.SetModerationStateInput) (*ca.AuthorStats, error) {
	if !in.State.Valid() {
		return nil, ca.ErrInvalidFilter
	}
	ws, id := strings.TrimSpace(in.WorkspaceID), strings.TrimSpace(in.AuthorID)
	if err := uc.authors.SetModerationState(ctx, ws, id, in.State, uc.clock.Now()); err != nil {
		return nil, err
	}
	return uc.authors.FindByID(ctx, ws, id)
}

// ---- settings ----

// AccountVerifier confirms an account belongs to the workspace before its
// settings are read or written. Registered per source by the channel.
type AccountVerifier interface {
	AccountBelongsTo(ctx context.Context, workspaceID, accountID string) (bool, error)
}

type settingsUseCases struct {
	settings  ca.SettingsRepository
	verifiers map[ca.Source]AccountVerifier
	clock     ca.Clock
}

// NewSettingsUseCases builds both the getter and the updater over one set of
// dependencies; they share the ownership check.
func NewSettingsUseCases(settings ca.SettingsRepository, verifiers map[ca.Source]AccountVerifier, clock ca.Clock) (ca.GetSettingsUseCase, ca.UpdateSettingsUseCase) {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	uc := &settingsUseCases{settings: settings, verifiers: verifiers, clock: clock}
	return uc, updateSettingsAdapter{uc}
}

func (uc *settingsUseCases) verify(ctx context.Context, workspaceID string, source ca.Source, accountID string) error {
	if !source.Valid() || strings.TrimSpace(accountID) == "" {
		return ca.ErrContainerInvalid
	}
	v, ok := uc.verifiers[source]
	if !ok {
		return ca.ErrContainerInvalid
	}
	owns, err := v.AccountBelongsTo(ctx, workspaceID, accountID)
	if err != nil {
		return err
	}
	if !owns {
		return ca.ErrNotFound
	}
	return nil
}

func (uc *settingsUseCases) Execute(ctx context.Context, workspaceID string, source ca.Source, accountID string) (*ca.Settings, error) {
	workspaceID, accountID = strings.TrimSpace(workspaceID), strings.TrimSpace(accountID)
	if err := uc.verify(ctx, workspaceID, source, accountID); err != nil {
		return nil, err
	}
	s, err := uc.settings.Find(ctx, source, accountID)
	if err != nil {
		if errors.Is(err, ca.ErrNotFound) {
			def := ca.NewSettings(workspaceID, source, accountID, ca.VerticalServices)
			return &def, nil
		}
		return nil, err
	}
	if s.WorkspaceID != workspaceID {
		return nil, ca.ErrNotFound
	}
	return s, nil
}

// updateSettingsUseCase is the same struct under the updater's interface.
func (uc *settingsUseCases) update(ctx context.Context, in ca.UpdateSettingsInput) (*ca.Settings, error) {
	current, err := uc.Execute(ctx, in.WorkspaceID, in.Source, in.AccountID)
	if err != nil {
		return nil, err
	}
	next := *current
	if in.Enabled != nil {
		next.Enabled = *in.Enabled
	}
	if in.Model != nil {
		next.Model = strings.TrimSpace(*in.Model)
	}
	if in.Vertical != nil && in.Vertical.Valid() && *in.Vertical != next.Vertical {
		// Changing the vertical reseeds the topics unless the caller sent
		// their own in the same request.
		next.Vertical = *in.Vertical
		if in.Topics == nil {
			next.Topics = ca.DefaultTopicsFor(next.Vertical)
		}
	}
	if in.Topics != nil {
		next.Topics = in.Topics.Normalize()
	}
	if in.ActionPolicy != nil {
		next.ActionPolicy = *in.ActionPolicy
	}
	if in.ReplyPolicy != nil {
		// The domain can express `auto` and its gate is tested, but no step
		// posts on that policy yet: this cut ships suggest-only, per the plan.
		// Accepting the value here would leave an operator with a switch that
		// silently does nothing. Delete this refusal when the pipeline's
		// auto-reply step lands, not before.
		if in.ReplyPolicy.Mode == ca.ReplyModeAuto {
			return nil, fmt.Errorf("%w: automatic replies are not available yet", ca.ErrInvalidFilter)
		}
		next.ReplyPolicy = *in.ReplyPolicy
	}
	if in.DailyCap != nil {
		next.DailyCap = *in.DailyCap
	}
	if in.Instructions != nil {
		next.Instructions, _ = ca.TruncateRunes(strings.TrimSpace(*in.Instructions), ca.MaxInstructionsRunes)
	}
	next.UpdatedAt = uc.clock.Now()
	next.Normalize()
	if err := next.Validate(); err != nil {
		return nil, err
	}
	if err := uc.settings.Save(ctx, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

// The updater interface has the same method name with a different
// signature, so it is exposed through a thin adapter type.
type updateSettingsAdapter struct{ *settingsUseCases }

func (a updateSettingsAdapter) Execute(ctx context.Context, in ca.UpdateSettingsInput) (*ca.Settings, error) {
	return a.settingsUseCases.update(ctx, in)
}

// ---- retry ----

type retryUseCase struct {
	repo  ca.Repository
	clock ca.Clock
}

func NewRetryUseCase(repo ca.Repository, clock ca.Clock) ca.RetryUseCase {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &retryUseCase{repo: repo, clock: clock}
}

func (uc *retryUseCase) Execute(ctx context.Context, workspaceID, id string) (*ca.Analysis, error) {
	row, err := uc.repo.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := row.Retry(uc.clock.Now()); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// ---- spend ----

type spendUseCase struct {
	batches ca.BatchRepository
	clock   ca.Clock
}

func NewSpendUseCase(batches ca.BatchRepository, clock ca.Clock) ca.SpendUseCase {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &spendUseCase{batches: batches, clock: clock}
}

func (uc *spendUseCase) Execute(ctx context.Context, workspaceID string, in ca.SpendInput) (*ca.BatchTotals, error) {
	now := uc.clock.Now().UTC()
	var from time.Time
	if in.Days > 0 {
		from = now.Add(-time.Duration(in.Days) * 24 * time.Hour)
	} else {
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return uc.batches.Totals(ctx, strings.TrimSpace(workspaceID), from, now.Add(time.Second))
}
