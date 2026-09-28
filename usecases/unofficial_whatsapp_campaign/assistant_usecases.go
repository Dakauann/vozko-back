package unofficial_whatsapp_campaign

import (
	"context"

	"vozko/domain/campaign"
	"vozko/domain/media"
	"vozko/domain/sheet"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type InstanceLookup interface {
	Instance(ctx context.Context, instanceID string) (*uw.Instance, error)
}

func usableInstance(ctx context.Context, instances InstanceLookup, workspaceID string, scope uw.DepartmentScope, instanceID string) (*uw.Instance, error) {
	instance, err := instances.Instance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if err := uwuc.EnsureVisible(instance, workspaceID, scope); err != nil {
		return nil, err
	}
	if err := ensureInstanceCanCampaign(instance); err != nil {
		return nil, err
	}
	return instance, nil
}

type campaignInstance struct{ instances InstanceLookup }

func NewCampaignInstanceUseCase(instances InstanceLookup) uwc.CampaignInstanceUseCase {
	return &campaignInstance{instances: instances}
}

func (uc *campaignInstance) Usable(ctx context.Context, workspaceID string, scope uw.DepartmentScope, instanceID string) (*uw.Instance, error) {
	return usableInstance(ctx, uc.instances, workspaceID, scope, instanceID)
}

type campaignAction struct {
	access   uwc.CampaignAccessUseCase
	dispatch uwc.DispatchCampaignUseCase
}

func NewCampaignActionUseCase(access uwc.CampaignAccessUseCase, dispatch uwc.DispatchCampaignUseCase) uwc.CampaignActionUseCase {
	return &campaignAction{access: access, dispatch: dispatch}
}

func (uc *campaignAction) Act(ctx context.Context, workspaceID string, scope uw.DepartmentScope, campaignID string, action campaign.Action) (*uwc.Campaign, error) {
	owned, err := uc.access.Owned(ctx, workspaceID, scope, campaignID)
	if err != nil {
		return nil, err
	}
	if err := uc.dispatch.Dispatch(ctx, uwc.DispatchCampaignInput{CampaignID: owned.ID, Action: action}); err != nil {
		return nil, err
	}
	return owned, nil
}

type importPreview struct{ files media.ReadMediaUseCase }

func NewImportPreviewUseCase(files media.ReadMediaUseCase) uwc.ImportPreviewUseCase {
	return &importPreview{files: files}
}

func (uc *importPreview) Preview(ctx context.Context, req uwc.ImportRequest) (*uwc.ImportPreview, error) {
	message := req.Message
	message.Normalize()
	if err := message.Validate(); err != nil {
		return nil, err
	}
	file, err := uc.files.Read(ctx, req.WorkspaceID, req.MediaID)
	if err != nil {
		return nil, err
	}
	result, err := campaign.ReadImport(sheet.Parse(file.Data), req.Mapping, message.ParameterCount(), uwc.NormalizeTarget)
	if err != nil {
		return nil, err
	}
	if result.TotalRows > uwc.MaxCampaignTargets {
		return nil, uwc.ErrCampaignTargetsTooMany
	}
	return &uwc.ImportPreview{ImportResult: *result}, nil
}
