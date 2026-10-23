package conversationnote

import (
	"errors"
	"net/http"
	"path"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

const (
	MaxImages         = 4
	MaxImageBytes     = 10 << 20
	defaultImageStem  = "imagem"
	maxImageStemRunes = 80
)

var (
	ErrTooManyImages  = errors.New("conversationnote: a note can carry at most 4 images")
	ErrImageRequired  = errors.New("conversationnote: the image is empty")
	ErrImageTooLarge  = errors.New("conversationnote: an image can have at most 10 MB")
	ErrImageType      = errors.New("conversationnote: only JPEG, PNG, WebP and GIF images can go in a note")
	ErrImageNotFound  = errors.New("conversationnote: the image was not found in this conversation")
	ErrStorageMissing = errors.New("conversationnote: image storage is not configured")
)

var imageExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type SniffedImage struct {
	MIME string
	Ext  string
}

func SniffImage(data []byte) (SniffedImage, error) {
	if len(data) == 0 {
		return SniffedImage{}, ErrImageRequired
	}
	if len(data) > MaxImageBytes {
		return SniffedImage{}, ErrImageTooLarge
	}
	mime := strings.SplitN(http.DetectContentType(data), ";", 2)[0]
	ext, ok := imageExtensions[mime]
	if !ok {
		return SniffedImage{}, ErrImageType
	}
	return SniffedImage{MIME: mime, Ext: ext}, nil
}

func (s SniffedImage) FileName(original string) string {
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(original), "\\", "/"))
	stem := strings.TrimSpace(strings.TrimSuffix(base, path.Ext(base)))
	if stem == "" || stem == "." || stem == "/" {
		stem = defaultImageStem
	}
	if runes := []rune(stem); len(runes) > maxImageStemRunes {
		stem = string(runes[:maxImageStemRunes])
	}
	return stem + s.Ext
}

func NormalizeImageIDs(ids []string) ([]string, error) {
	out := shared.DistinctTrimmed(ids)
	if len(out) > MaxImages {
		return nil, ErrTooManyImages
	}
	return out, nil
}

func Attachable(media *conversation.ConversationMedia, entry shared.EntryRef) error {
	if media == nil || !media.BelongsTo(entry.EntryID, entry.EntryType) || media.Type != conversation.MediaTypeImage {
		return ErrImageNotFound
	}
	if _, ok := imageExtensions[media.MimeType]; !ok {
		return ErrImageType
	}
	if media.SizeBytes <= 0 || media.SizeBytes > MaxImageBytes {
		return ErrImageTooLarge
	}
	return nil
}

type Image struct {
	ID       string
	URL      string
	MimeType string
	Layout   conversation.MediaLayout
}

func ImageOf(media *conversation.ConversationMedia) Image {
	return Image{ID: media.ID, URL: media.URL, MimeType: media.MimeType, Layout: media.Layout}
}

func AttachedImages(ids []string, media []*conversation.ConversationMedia, entry shared.EntryRef) []Image {
	byID := make(map[string]*conversation.ConversationMedia, len(media))
	for _, m := range media {
		if m != nil {
			byID[m.ID] = m
		}
	}
	out := make([]Image, 0, len(ids))
	for _, id := range ids {
		m := byID[id]
		if Attachable(m, entry) == nil {
			out = append(out, ImageOf(m))
		}
	}
	return out
}
