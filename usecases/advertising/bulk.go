package advertising

import (
	"context"

	ads "vozko/domain/advertising"
)

type bulkManager interface {
	CheckStatus(ctx context.Context, workspaceID, metaID string, on bool) (*ads.Object, error)
	SetStatus(ctx context.Context, workspaceID, metaID string, on bool) (*ads.Object, error)
	Detail(ctx context.Context, workspaceID, metaID string) (*ads.ObjectDetail, error)
	CheckEdit(ctx context.Context, workspaceID, metaID string, edit ads.ObjectEdit) (*ads.ObjectDetail, error)
	Edit(ctx context.Context, workspaceID, metaID string, edit ads.ObjectEdit) (*ads.Object, error)
}

type BulkUseCase struct {
	manage bulkManager
}

func NewBulkUseCase(manage bulkManager) *BulkUseCase {
	return &BulkUseCase{manage: manage}
}

type BulkResult struct {
	MetaID string
	Object *ads.Object
	Err    error
}

func (uc *BulkUseCase) CheckStatus(ctx context.Context, workspaceID string, metaIDs []string, on bool) ([]BulkResult, error) {
	return each(metaIDs, func(id string) (*ads.Object, error) {
		return uc.manage.CheckStatus(ctx, workspaceID, id, on)
	})
}

func (uc *BulkUseCase) SetStatus(ctx context.Context, workspaceID string, metaIDs []string, on bool) ([]BulkResult, error) {
	return each(metaIDs, func(id string) (*ads.Object, error) {
		return uc.manage.SetStatus(ctx, workspaceID, id, on)
	})
}

func (uc *BulkUseCase) editOf(ctx context.Context, workspaceID, id string, change ads.BulkChange) (ads.ObjectEdit, error) {
	detail, err := uc.manage.Detail(ctx, workspaceID, id)
	if err != nil {
		return ads.ObjectEdit{}, err
	}
	return change.EditFor(*detail)
}

func (uc *BulkUseCase) CheckEdit(ctx context.Context, workspaceID string, metaIDs []string, change ads.BulkChange) ([]BulkResult, error) {
	if err := change.Validate(); err != nil {
		return nil, err
	}
	return each(metaIDs, func(id string) (*ads.Object, error) {
		edit, err := uc.editOf(ctx, workspaceID, id, change)
		if err != nil {
			return nil, err
		}
		return uc.checked(ctx, workspaceID, id, edit)
	})
}

func (uc *BulkUseCase) Edit(ctx context.Context, workspaceID string, metaIDs []string, change ads.BulkChange) ([]BulkResult, error) {
	if err := change.Validate(); err != nil {
		return nil, err
	}
	return each(metaIDs, func(id string) (*ads.Object, error) {
		edit, err := uc.editOf(ctx, workspaceID, id, change)
		if err != nil {
			return nil, err
		}
		return uc.manage.Edit(ctx, workspaceID, id, edit)
	})
}

func (uc *BulkUseCase) itemEdit(ctx context.Context, workspaceID, id string, edit ads.ObjectEdit) (ads.ObjectEdit, error) {
	if !edit.NeedsCurrentBudget() {
		return edit, nil
	}
	detail, err := uc.manage.Detail(ctx, workspaceID, id)
	if err != nil {
		return ads.ObjectEdit{}, err
	}
	return edit.For(*detail), nil
}

func (uc *BulkUseCase) CheckApply(ctx context.Context, workspaceID string, metaIDs []string, edit ads.ObjectEdit) ([]BulkResult, error) {
	if edit.Empty() {
		return nil, ads.ErrNothingToChange
	}
	return each(metaIDs, func(id string) (*ads.Object, error) {
		itemEdit, err := uc.itemEdit(ctx, workspaceID, id, edit)
		if err != nil {
			return nil, err
		}
		return uc.checked(ctx, workspaceID, id, itemEdit)
	})
}

func (uc *BulkUseCase) Apply(ctx context.Context, workspaceID string, metaIDs []string, edit ads.ObjectEdit) ([]BulkResult, error) {
	if edit.Empty() {
		return nil, ads.ErrNothingToChange
	}
	return each(metaIDs, func(id string) (*ads.Object, error) {
		itemEdit, err := uc.itemEdit(ctx, workspaceID, id, edit)
		if err != nil {
			return nil, err
		}
		return uc.manage.Edit(ctx, workspaceID, id, itemEdit)
	})
}

func (uc *BulkUseCase) checked(ctx context.Context, workspaceID, id string, edit ads.ObjectEdit) (*ads.Object, error) {
	detail, err := uc.manage.CheckEdit(ctx, workspaceID, id, edit)
	if err != nil {
		return nil, err
	}
	return detail.Object, nil
}

func each(metaIDs []string, run func(id string) (*ads.Object, error)) ([]BulkResult, error) {
	if err := ads.ValidateBulkTargets(metaIDs); err != nil {
		return nil, err
	}
	results := make([]BulkResult, 0, len(metaIDs))
	for _, id := range metaIDs {
		object, err := run(id)
		results = append(results, BulkResult{MetaID: id, Object: object, Err: err})
	}
	return results, nil
}
