package unofficial_whatsapp

import (
	"log"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

type seedAsset struct {
	url       string
	mimeType  string
	fileName  string
	mediaType conversation.MediaType
}

func (uc *SeedInboxUseCase) resolveAsset(in uw.SeedRequest) *seedAsset {
	if in.Script == nil || in.Script.Attachment == nil {
		return nil
	}
	attachment := in.Script.Attachment

	if uc.assets == nil || uc.attachments == nil {
		log.Printf("[unofficial-whatsapp] seed attachment %s skipped: media stores are not wired on this deployment",
			attachment.MediaID)
		return nil
	}

	asset, err := uc.assets.GetMediaByID(attachment.MediaID)
	if err != nil || asset == nil || strings.TrimSpace(asset.URL) == "" {
		log.Printf("[unofficial-whatsapp] seed attachment %s could not be resolved, seeding the opening plain: %v",
			attachment.MediaID, err)
		return nil
	}

	if asset.WorkspaceID != in.WorkspaceID {
		log.Printf("[unofficial-whatsapp] seed attachment %s belongs to another workspace, seeding the opening plain",
			attachment.MediaID)
		return nil
	}

	url := strings.TrimSpace(asset.URL)
	return &seedAsset{
		url:       url,
		mimeType:  mime.TypeByExtension(path.Ext(url)),
		fileName:  assetFileName(asset.Description, url),
		mediaType: conversationMediaType(attachment.Kind),
	}
}

func (uc *SeedInboxUseCase) attach(message *conversation.Message, asset *seedAsset, now time.Time) {
	if message == nil || asset == nil || uc.attachments == nil {
		return
	}

	row := &conversation.ConversationMedia{
		ID:               uuid.NewString(),
		EntryID:          message.EntryID,
		EntryType:        shared.EntryTypeUnofficialWhatsApp,
		Type:             asset.mediaType,
		MimeType:         asset.mimeType,
		URL:              asset.url,
		OriginalFilename: asset.fileName,
		CreatedAt:        now.UTC(),
	}
	row.Normalize()
	if err := row.Validate(); err != nil {
		log.Printf("[unofficial-whatsapp] seed attachment for conversation %s is unusable: %v", message.EntryID, err)
		return
	}
	if err := uc.attachments.Create(row); err != nil {
		log.Printf("[unofficial-whatsapp] could not attach the seeded file to conversation %s: %v",
			message.EntryID, err)
		return
	}

	message.MediaID = &row.ID
	message.MediaType = asset.mediaType
}

func assetFileName(description, url string) string {
	if name := strings.TrimSpace(description); name != "" {
		return name
	}
	return path.Base(url)
}
