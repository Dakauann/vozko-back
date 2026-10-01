package conversationad

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type Recorder struct {
	origins     conversation.AdOriginRepository
	media       conversation.MediaStore
	fetcher     conversation.RemoteFileFetcher
	broadcaster conversation.AdOriginBroadcaster
	now         func() time.Time
}

func NewRecorder(
	origins conversation.AdOriginRepository,
	media conversation.MediaStore,
	fetcher conversation.RemoteFileFetcher,
	broadcaster conversation.AdOriginBroadcaster,
	now func() time.Time,
) *Recorder {
	return &Recorder{origins: origins, media: media, fetcher: fetcher, broadcaster: broadcaster, now: now}
}

func (r *Recorder) Record(ctx context.Context, entryID string, entryType shared.EntryType, ad *conversation.AdReferral) {
	if !ad.Usable() || strings.TrimSpace(entryID) == "" {
		return
	}
	existing, err := r.origins.Get(entryID, entryType)
	if err != nil {
		log.Printf("[ad-origin] lookup for %s:%s failed: %v", entryType, entryID, err)
		return
	}
	if existing != nil {
		return
	}
	origin := &conversation.AdOrigin{
		EntryID:   entryID,
		EntryType: entryType,
		AdID:      strings.TrimSpace(ad.AdID),
		Platform:  ad.Platform,
		Title:     strings.TrimSpace(ad.Title),
		SourceURL: strings.TrimSpace(ad.SourceURL),
		ArrivedAt: r.now().UTC(),
	}
	if image := r.storeImage(ctx, entryID, entryType, ad); image != nil {
		origin.ImageMediaID = image.ID
		origin.Image = image.Attachment()
	}
	claimed, err := r.origins.Claim(origin)
	if err != nil {
		log.Printf("[ad-origin] saving the ad of %s:%s failed: %v", entryType, entryID, err)
		return
	}
	if claimed && r.broadcaster != nil {
		r.broadcaster.BroadcastAdOrigin(entryID, entryType, origin)
	}
}

func (r *Recorder) storeImage(ctx context.Context, entryID string, entryType shared.EntryType, ad *conversation.AdReferral) *conversation.ConversationMedia {
	if r.media == nil {
		return nil
	}
	data := ad.Image
	if len(data) == 0 && r.fetcher != nil && strings.TrimSpace(ad.ImageURL) != "" {
		fetched, err := r.fetcher.Fetch(ctx, ad.ImageURL)
		if err != nil {
			log.Printf("[ad-origin] ad image of %s:%s not downloaded: %v", entryType, entryID, err)
			return nil
		}
		data = fetched
	}
	if len(data) == 0 {
		return nil
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		log.Printf("[ad-origin] ad image of %s:%s is %s, not an image", entryType, entryID, mimeType)
		return nil
	}
	id := uuid.NewString()
	stored, err := r.media.Store(conversation.StoreMediaInput{
		ID:        id,
		Key:       "conversations/" + string(entryType) + "/" + entryID + "/ad-" + id,
		EntryID:   entryID,
		EntryType: entryType,
		Type:      conversation.MediaTypeImage,
		MimeType:  mimeType,
		Data:      data,
	})
	if err != nil {
		log.Printf("[ad-origin] ad image of %s:%s not stored: %v", entryType, entryID, err)
		return nil
	}
	return stored
}
