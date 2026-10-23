package stage_usecase

import "vozko/domain/stage"

type announcingRepository struct {
	stage.Repository
	listener stage.EnteredListener
}

func AnnounceEntries(repo stage.Repository, listener stage.EnteredListener) stage.Repository {
	return &announcingRepository{Repository: repo, listener: listener}
}

func (r *announcingRepository) AssignStage(et *stage.EntryStage) (string, error) {
	previous, err := r.Repository.AssignStage(et)
	if err != nil {
		return previous, err
	}
	r.announce(et, previous)
	return previous, nil
}

func (r *announcingRepository) AssignStageIfNone(et *stage.EntryStage) (bool, error) {
	assigned, err := r.Repository.AssignStageIfNone(et)
	if err != nil || !assigned {
		return assigned, err
	}
	r.announce(et, "")
	return true, nil
}

func (r *announcingRepository) announce(et *stage.EntryStage, previous string) {
	if entered, ok := stage.EnteredBy(et, previous); ok && r.listener != nil {
		r.listener.StageEntered(entered)
	}
}
