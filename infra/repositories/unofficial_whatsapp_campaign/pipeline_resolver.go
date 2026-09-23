package unofficial_whatsapp_campaign_repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/stage"
	"vozko/infra/database/schema"
)

type pipelineResolver struct {
	db *gorm.DB
}

func NewContainerPipelineResolver(db *gorm.DB) stage.ContainerPipelineResolver {
	return &pipelineResolver{db: db}
}

func (r *pipelineResolver) PipelineIDForContainer(ctx context.Context, containerID string) (string, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", nil
	}

	campaignPipeline, err := r.pipelineFrom(ctx, &schema.UnofficialWhatsAppCampaign{}, containerID)
	if err != nil {
		return "", err
	}
	if campaignPipeline != "" {
		return campaignPipeline, nil
	}

	return r.pipelineFrom(ctx, &schema.UnofficialWhatsAppInstance{}, containerID)
}

func (r *pipelineResolver) pipelineFrom(ctx context.Context, model interface{}, containerID string) (string, error) {
	var row struct {
		PipelineID *string `gorm:"column:pipeline_id"`
	}
	err := r.db.WithContext(ctx).Model(model).
		Select("pipeline_id").
		Where("id = ? AND deleted_at IS NULL", containerID).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return "", err
	}
	if row.PipelineID == nil {
		return "", nil
	}
	return strings.TrimSpace(*row.PipelineID), nil
}
