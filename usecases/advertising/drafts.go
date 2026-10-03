package advertising

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	ads "vozko/domain/advertising"
)

type draftPublisher interface {
	Publish(ctx context.Context, in PublishInput) (*ads.PublishJob, error)
}

type DraftsUseCase struct {
	drafts    ads.SavedDraftRepository
	accounts  ads.AccountRepository
	jobs      ads.PublishJobRepository
	publisher draftPublisher
	newJobID  func() string
	now       func() time.Time
}

func NewDraftsUseCase(drafts ads.SavedDraftRepository, accounts ads.AccountRepository, jobs ads.PublishJobRepository, publisher draftPublisher) *DraftsUseCase {
	return &DraftsUseCase{drafts: drafts, accounts: accounts, jobs: jobs, publisher: publisher, newJobID: uuid.NewString, now: time.Now}
}

type DraftView struct {
	Draft *ads.SavedDraft
	State ads.DraftState
	Job   *ads.PublishJob
}

type DraftList struct {
	Drafts      []DraftView
	ObjectCount int
}

func (uc *DraftsUseCase) List(ctx context.Context, workspaceID, accountID string) (*DraftList, error) {
	if _, err := uc.accounts.FindByID(ctx, workspaceID, accountID); err != nil {
		return nil, err
	}
	drafts, err := uc.drafts.ListByAccount(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	out := &DraftList{Drafts: make([]DraftView, 0, len(drafts))}
	for _, d := range drafts {
		view, err := uc.view(ctx, d)
		if err != nil {
			return nil, err
		}
		if view.State == ads.DraftPublished {
			uc.forget(ctx, d)
			continue
		}
		out.Drafts = append(out.Drafts, *view)
		out.ObjectCount += len(d.Rows())
	}
	return out, nil
}

func (uc *DraftsUseCase) Get(ctx context.Context, workspaceID, id string) (*DraftView, error) {
	d, err := uc.drafts.Find(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return uc.view(ctx, d)
}

func (uc *DraftsUseCase) Create(ctx context.Context, workspaceID, userID string, content ads.AdDraft) (*DraftView, error) {
	account, err := uc.accounts.FindByID(ctx, workspaceID, content.AdAccountID)
	if err != nil {
		return nil, err
	}
	d, err := ads.NewSavedDraft(workspaceID, account.ID, userID, content)
	if err != nil {
		return nil, err
	}
	if err := uc.drafts.Create(ctx, d); err != nil {
		return nil, err
	}
	return &DraftView{Draft: d, State: ads.DraftEditing}, nil
}

func (uc *DraftsUseCase) Update(ctx context.Context, workspaceID, userID, id string, version int, content ads.AdDraft) (*DraftView, error) {
	view, err := uc.CheckEdit(ctx, workspaceID, id, version)
	if err != nil {
		return nil, err
	}
	d := view.Draft
	account, err := uc.accounts.FindByID(ctx, workspaceID, content.AdAccountID)
	if err != nil {
		return nil, err
	}
	if err := d.Replace(account.ID, userID, content); err != nil {
		return nil, err
	}
	if err := uc.drafts.Save(ctx, d); err != nil {
		return nil, err
	}
	return &DraftView{Draft: d, State: ads.DraftEditing}, nil
}

func (uc *DraftsUseCase) Duplicate(ctx context.Context, workspaceID, userID, id, name string) (*DraftView, error) {
	original, err := uc.drafts.Find(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	copied, err := original.Copy(userID, name)
	if err != nil {
		return nil, err
	}
	if err := uc.drafts.Create(ctx, copied); err != nil {
		return nil, err
	}
	return &DraftView{Draft: copied, State: ads.DraftEditing}, nil
}

func (uc *DraftsUseCase) Delete(ctx context.Context, workspaceID, id string) error {
	view, err := uc.editable(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	return uc.drafts.Delete(ctx, view.Draft)
}

func (uc *DraftsUseCase) Discard(ctx context.Context, workspaceID, accountID string) (int, error) {
	list, err := uc.List(ctx, workspaceID, accountID)
	if err != nil {
		return 0, err
	}
	discarded := 0
	for _, view := range list.Drafts {
		if view.Draft.Editable(view.Job, uc.now()) != nil {
			continue
		}
		err := uc.drafts.Delete(ctx, view.Draft)
		if errors.Is(err, ads.ErrDraftNotFound) || errors.Is(err, ads.ErrDraftChanged) {
			continue
		}
		if err != nil {
			return discarded, err
		}
		discarded++
	}
	return discarded, nil
}

func (uc *DraftsUseCase) CheckEdit(ctx context.Context, workspaceID, id string, version int) (*DraftView, error) {
	view, err := uc.editable(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if view.Draft.Version != version {
		return nil, ads.ErrDraftChanged
	}
	return view, nil
}

func (uc *DraftsUseCase) Publish(ctx context.Context, workspaceID, userID, id string, version int, actor ads.Actor) (*ads.PublishJob, error) {
	view, err := uc.CheckEdit(ctx, workspaceID, id, version)
	if err != nil {
		return nil, err
	}
	d := view.Draft
	jobID := uc.newJobID()
	if err := uc.drafts.ClaimForPublish(ctx, d, jobID); err != nil {
		return nil, err
	}
	job, err := uc.publisher.Publish(ctx, PublishInput{
		JobID: jobID, WorkspaceID: workspaceID, UserID: userID, Actor: actor, Draft: d.Content,
	})
	if job == nil {
		if releaseErr := uc.drafts.ReleaseJob(ctx, workspaceID, d.ID, jobID); releaseErr != nil {
			log.Printf("[ads] draft %s kept a job that was never created: %v", d.ID, releaseErr)
		}
		return nil, err
	}
	if d.State(job, uc.now()) == ads.DraftPublished {
		uc.forget(ctx, d)
	}
	return job, err
}

func (uc *DraftsUseCase) editable(ctx context.Context, workspaceID, id string) (*DraftView, error) {
	view, err := uc.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := view.Draft.Editable(view.Job, uc.now()); err != nil {
		return nil, err
	}
	return view, nil
}

func (uc *DraftsUseCase) view(ctx context.Context, d *ads.SavedDraft) (*DraftView, error) {
	if d.JobID == "" {
		return &DraftView{Draft: d, State: d.State(nil, uc.now())}, nil
	}
	job, err := uc.jobs.Find(ctx, d.WorkspaceID, d.JobID)
	if errors.Is(err, ads.ErrJobNotFound) {
		job, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &DraftView{Draft: d, State: d.State(job, uc.now()), Job: job}, nil
}

func (uc *DraftsUseCase) forget(ctx context.Context, d *ads.SavedDraft) {
	if err := uc.drafts.Delete(ctx, d); err != nil && !errors.Is(err, ads.ErrDraftNotFound) && !errors.Is(err, ads.ErrDraftChanged) {
		log.Printf("[ads] published draft %s could not be removed: %v", d.ID, err)
	}
}
