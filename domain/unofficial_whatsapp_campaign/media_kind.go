package unofficial_whatsapp_campaign

import "vozko/domain/media"

func KindForMedia(t media.MediaType) MessageKind {
	switch t {
	case media.MediaTypeProductImage, media.MediaTypeSticker:
		return KindImage
	case media.MediaTypeProductVideo, media.MediaTypeVslVideo:
		return KindVideo
	case media.MediaTypeAudio:
		return KindAudio
	default:
		return KindDocument
	}
}
