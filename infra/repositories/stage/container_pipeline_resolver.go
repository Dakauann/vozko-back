package stage_repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/stage"
	"vozko/infra/database/schema"
)

type containerPipelineResolver struct {
	db     *gorm.DB
	models []func() any
}

func NewContainerPipelineResolver(db *gorm.DB, models ...func() any) stage.ContainerPipelineResolver {
	return &containerPipelineResolver{db: db, models: models}
}

func ChannelPipelineResolvers(db *gorm.DB) map[shared.EntryType]stage.ContainerPipelineResolver {
	return map[shared.EntryType]stage.ContainerPipelineResolver{
		shared.EntryTypeWhatsApp:  NewContainerPipelineResolver(db, func() any { return &schema.WhatsAppCampaign{} }),
		shared.EntryTypeInstagram: NewContainerPipelineResolver(db, func() any { return &schema.InstagramAccount{} }),
		shared.EntryTypeTelegram:  NewContainerPipelineResolver(db, func() any { return &schema.TelegramAccount{} }),
		shared.EntryTypeFacebook:  NewContainerPipelineResolver(db, func() any { return &schema.FacebookPage{} }),
		shared.EntryTypeWebchat:   NewContainerPipelineResolver(db, func() any { return &schema.WebchatWidget{} }),
		shared.EntryTypeUnofficialWhatsApp: NewContainerPipelineResolver(db,
			func() any { return &schema.UnofficialWhatsAppCampaign{} },
			func() any { return &schema.UnofficialWhatsAppInstance{} }),
	}
}

func (r *containerPipelineResolver) PipelineIDForContainer(ctx context.Context, containerID string) (string, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", nil
	}
	for _, model := range r.models {
		var row struct {
			PipelineID *string `gorm:"column:pipeline_id"`
		}
		if err := r.db.WithContext(ctx).Model(model()).
			Select("pipeline_id").
			Where("id = ?", containerID).
			Limit(1).
			Scan(&row).Error; err != nil {
			return "", err
		}
		if row.PipelineID != nil && strings.TrimSpace(*row.PipelineID) != "" {
			return strings.TrimSpace(*row.PipelineID), nil
		}
	}
	return "", nil
}
