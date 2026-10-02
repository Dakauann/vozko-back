package advertising

import (
	"context"
	"log"

	ads "vozko/domain/advertising"
)

type manageGateway interface {
	mediaGateway
	SetStatus(ctx context.Context, token, metaID string, status ads.ConfiguredStatus) error
	DeleteObject(ctx context.Context, token, metaID string) error
	GetObjectDetail(ctx context.Context, token, metaID string, level ads.Level) (*ads.ObjectDetail, error)
	UpdateObject(ctx context.Context, token, metaID string, level ads.Level, spec ads.EditSpec) error
	CopyObject(ctx context.Context, token, metaID string, level ads.Level, req ads.CopyRequest) (string, error)
	CreateCreative(ctx context.Context, token, metaAccountID string, spec ads.CreativeSpec) (string, error)
	SetSpendCap(ctx context.Context, token, metaAccountID string, cap int64) error
	RemoveSpendCap(ctx context.Context, token, metaAccountID string) error
}

type ManageUseCase struct {
	access  accountAccess
	gateway manageGateway
	objects ads.ObjectRepository
	media   creativeMedia
	sync    *SyncUseCase
}

func NewManageUseCase(sync *SyncUseCase, gateway manageGateway, source MediaSource) *ManageUseCase {
	return &ManageUseCase{access: sync.access, gateway: gateway, objects: sync.objects, media: newCreativeMedia(source, gateway), sync: sync}
}

type target struct {
	object  *ads.Object
	account *ads.AdAccount
	token   string
}

func (uc *ManageUseCase) target(ctx context.Context, workspaceID, metaID string) (*target, error) {
	object, err := uc.objects.Find(ctx, workspaceID, metaID)
	if err != nil {
		return nil, err
	}
	account, token, err := uc.access.open(ctx, workspaceID, object.AdAccountID, ads.ScopeAdsManagement)
	if err != nil {
		return nil, err
	}
	return &target{object: object, account: account, token: token}, nil
}

func (uc *ManageUseCase) refresh(ctx context.Context, t *target, workspaceID string) (*ads.Object, error) {
	if err := uc.sync.SyncStructure(ctx, t.account, t.token); err != nil {
		log.Printf("[ads] %s changed but the refresh failed: %v", t.object.MetaID, err)
		return nil, err
	}
	return uc.objects.Find(ctx, workspaceID, t.object.MetaID)
}

func (uc *ManageUseCase) statusTarget(ctx context.Context, workspaceID, metaID string, on bool) (*target, ads.ConfiguredStatus, error) {
	t, err := uc.target(ctx, workspaceID, metaID)
	if err != nil {
		return nil, "", err
	}
	if err := t.object.CanToggle(); err != nil {
		return nil, "", err
	}
	if !on {
		return t, ads.StatusPaused, nil
	}
	if err := t.account.CanSpend(); err != nil {
		return nil, "", err
	}
	return t, ads.StatusActive, nil
}

func (uc *ManageUseCase) CheckStatus(ctx context.Context, workspaceID, metaID string, on bool) (*ads.Object, error) {
	t, _, err := uc.statusTarget(ctx, workspaceID, metaID, on)
	if err != nil {
		return nil, err
	}
	return t.object, nil
}

func (uc *ManageUseCase) SetStatus(ctx context.Context, workspaceID, metaID string, on bool) (*ads.Object, error) {
	t, status, err := uc.statusTarget(ctx, workspaceID, metaID, on)
	if err != nil {
		return nil, err
	}
	if err := uc.gateway.SetStatus(ctx, t.token, metaID, status); err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	refreshed, err := uc.refresh(ctx, t, workspaceID)
	if err != nil {
		t.object.Status = status
		return t.object, uc.objects.Upsert(ctx, t.object)
	}
	return refreshed, nil
}

func (uc *ManageUseCase) Detail(ctx context.Context, workspaceID, metaID string) (*ads.ObjectDetail, error) {
	t, err := uc.target(ctx, workspaceID, metaID)
	if err != nil {
		return nil, err
	}
	detail, err := uc.gateway.GetObjectDetail(ctx, t.token, metaID, t.object.Level)
	if err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	detail.Object = t.object
	return detail, nil
}

type editPlan struct {
	target *target
	detail *ads.ObjectDetail
	edit   ads.ObjectEdit
}

func (uc *ManageUseCase) planEdit(ctx context.Context, workspaceID, metaID string, edit ads.ObjectEdit) (*editPlan, error) {
	edit.Normalize()
	t, err := uc.target(ctx, workspaceID, metaID)
	if err != nil {
		return nil, err
	}
	detail, err := uc.gateway.GetObjectDetail(ctx, t.token, metaID, t.object.Level)
	if err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	detail.Object = t.object
	restricted, err := uc.restrictedCategory(ctx, workspaceID, t.object)
	if err != nil {
		return nil, err
	}
	if err := edit.Validate(*detail, restricted, uc.access.now()); err != nil {
		return nil, err
	}
	if edit.Creative != nil {
		for _, ref := range edit.Creative.MediaRefs() {
			if _, err := uc.media.describe(ctx, workspaceID, ref); err != nil {
				return nil, ads.FieldError("creative.media", "not_found")
			}
		}
	}
	return &editPlan{target: t, detail: detail, edit: edit}, nil
}

func (uc *ManageUseCase) CheckEdit(ctx context.Context, workspaceID, metaID string, edit ads.ObjectEdit) (*ads.ObjectDetail, error) {
	plan, err := uc.planEdit(ctx, workspaceID, metaID, edit)
	if err != nil {
		return nil, err
	}
	return plan.detail, nil
}

func (uc *ManageUseCase) Edit(ctx context.Context, workspaceID, metaID string, edit ads.ObjectEdit) (*ads.Object, error) {
	plan, err := uc.planEdit(ctx, workspaceID, metaID, edit)
	if err != nil {
		return nil, err
	}
	t := plan.target
	creativeID := ""
	if plan.edit.Creative != nil {
		creativeID, err = uc.createCreative(ctx, workspaceID, t, plan)
		if err != nil {
			return nil, err
		}
	}
	if err := uc.gateway.UpdateObject(ctx, t.token, metaID, t.object.Level, ads.EditSpecOf(plan.edit, creativeID)); err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	if plan.edit.Budget != nil {
		t.object.RecordBudgetChange(uc.access.now())
		if err := uc.objects.Upsert(ctx, t.object); err != nil {
			return nil, err
		}
	}
	refreshed, err := uc.refresh(ctx, t, workspaceID)
	if err != nil {
		return t.object, nil
	}
	return refreshed, nil
}

func (uc *ManageUseCase) createCreative(ctx context.Context, workspaceID string, t *target, plan *editPlan) (string, error) {
	creative := *plan.edit.Creative
	uploaded, err := uc.media.uploadAll(ctx, workspaceID, t.account, t.token, creative)
	if err != nil {
		return "", uc.access.failed(ctx, t.account, err)
	}
	destination := ads.Destination(t.object.DestinationType)
	if adSet, err := uc.objects.Find(ctx, workspaceID, t.object.AdSetMetaID); err == nil {
		destination = ads.Destination(adSet.DestinationType)
	}
	spec := ads.CreativeSpec{
		Name: t.object.Name, Identity: plan.detail.Identity, Destination: destination,
		CallToAction: creative.ResolvedCallToAction(destination), Creative: creative, Media: uploaded,
	}
	id, err := uc.gateway.CreateCreative(ctx, t.token, t.account.MetaAccountID, spec)
	if err != nil {
		return "", uc.access.failed(ctx, t.account, err)
	}
	return id, nil
}

func (uc *ManageUseCase) SetBudget(ctx context.Context, workspaceID, metaID string, amount int64) (*ads.Object, error) {
	object, err := uc.objects.Find(ctx, workspaceID, metaID)
	if err != nil {
		return nil, err
	}
	current := object.Budget()
	if current == nil {
		return nil, ads.ErrNoBudget
	}
	return uc.Edit(ctx, workspaceID, metaID, ads.ObjectEdit{Budget: &ads.Budget{Kind: current.Kind, Amount: amount}})
}

func (uc *ManageUseCase) CheckBudget(ctx context.Context, workspaceID, metaID string, amount int64) (*ads.Object, *ads.AdAccount, error) {
	object, err := uc.objects.Find(ctx, workspaceID, metaID)
	if err != nil {
		return nil, nil, err
	}
	current := object.Budget()
	if current == nil {
		return nil, nil, ads.ErrNoBudget
	}
	plan, err := uc.planEdit(ctx, workspaceID, metaID, ads.ObjectEdit{Budget: &ads.Budget{Kind: current.Kind, Amount: amount}})
	if err != nil {
		return nil, nil, err
	}
	return plan.target.object, plan.target.account, nil
}

func (uc *ManageUseCase) copyTarget(ctx context.Context, workspaceID, metaID string, req ads.CopyRequest) (*target, error) {
	t, err := uc.target(ctx, workspaceID, metaID)
	if err != nil {
		return nil, err
	}
	if err := req.Validate(t.object); err != nil {
		return nil, err
	}
	if req.ParentID != "" {
		parent, err := uc.objects.Find(ctx, workspaceID, req.ParentID)
		if err != nil || parent.AdAccountID != t.object.AdAccountID || !validCopyParent(t.object.Level, parent.Level) {
			return nil, ads.FieldError("parentId", "not_available")
		}
	}
	return t, nil
}

func (uc *ManageUseCase) CheckCopy(ctx context.Context, workspaceID, metaID string, req ads.CopyRequest) (*ads.Object, error) {
	t, err := uc.copyTarget(ctx, workspaceID, metaID, req)
	if err != nil {
		return nil, err
	}
	return t.object, nil
}

func (uc *ManageUseCase) Copy(ctx context.Context, workspaceID, metaID string, req ads.CopyRequest) (string, error) {
	t, err := uc.copyTarget(ctx, workspaceID, metaID, req)
	if err != nil {
		return "", err
	}
	id, err := uc.gateway.CopyObject(ctx, t.token, metaID, t.object.Level, req)
	if err != nil {
		return "", uc.access.failed(ctx, t.account, err)
	}
	if _, err := uc.refresh(ctx, t, workspaceID); err != nil {
		log.Printf("[ads] copy %s created but not mirrored yet: %v", id, err)
	}
	return id, nil
}

func validCopyParent(level, parentLevel ads.Level) bool {
	return (level == ads.LevelAdSet && parentLevel == ads.LevelCampaign) || (level == ads.LevelAd && parentLevel == ads.LevelAdSet)
}

func (uc *ManageUseCase) lifecycleTarget(ctx context.Context, workspaceID, metaID string, action ads.Lifecycle) (*target, error) {
	t, err := uc.target(ctx, workspaceID, metaID)
	if err != nil {
		return nil, err
	}
	if err := action.Check(t.object); err != nil {
		return nil, err
	}
	return t, nil
}

func (uc *ManageUseCase) CheckLifecycle(ctx context.Context, workspaceID, metaID string, action ads.Lifecycle) (*ads.Object, error) {
	t, err := uc.lifecycleTarget(ctx, workspaceID, metaID, action)
	if err != nil {
		return nil, err
	}
	return t.object, nil
}

func (uc *ManageUseCase) Lifecycle(ctx context.Context, workspaceID, metaID string, action ads.Lifecycle) (*ads.Object, error) {
	t, err := uc.lifecycleTarget(ctx, workspaceID, metaID, action)
	if err != nil {
		return nil, err
	}
	if action == ads.LifecycleDelete {
		err = uc.gateway.DeleteObject(ctx, t.token, metaID)
	} else {
		err = uc.gateway.SetStatus(ctx, t.token, metaID, ads.StatusArchived)
	}
	if err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	refreshed, err := uc.refresh(ctx, t, workspaceID)
	if err != nil {
		return t.object, nil
	}
	return refreshed, nil
}

func (uc *ManageUseCase) SetSpendCap(ctx context.Context, workspaceID, accountID string, cap *int64) (*ads.AdAccount, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.ScopeAdsManagement)
	if err != nil {
		return nil, err
	}
	if cap == nil {
		err = uc.gateway.RemoveSpendCap(ctx, token, account.MetaAccountID)
	} else if err = ads.ValidateSpendCap(*cap, account.AmountSpent); err == nil {
		err = uc.gateway.SetSpendCap(ctx, token, account.MetaAccountID, *cap)
	}
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	if err := uc.sync.syncAccount(ctx, account, token, RecentInsightDays); err != nil {
		log.Printf("[ads] spend cap of %s changed but the refresh failed: %v", account.ID, err)
	}
	return account, nil
}

func (uc *ManageUseCase) restrictedCategory(ctx context.Context, workspaceID string, o *ads.Object) (bool, error) {
	if o.Level == ads.LevelCampaign {
		return o.SpecialCategory.Restricted(), nil
	}
	campaign, err := uc.objects.Find(ctx, workspaceID, o.CampaignMetaID)
	if err != nil {
		return false, err
	}
	return campaign.SpecialCategory.Restricted(), nil
}
