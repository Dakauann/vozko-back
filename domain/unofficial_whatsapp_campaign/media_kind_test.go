package unofficial_whatsapp_campaign

import (
	"testing"

	"vozko/domain/media"
)

func TestKindForMediaSendsEachFileAsWhatItIs(t *testing.T) {
	cases := map[media.MediaType]MessageKind{
		media.MediaTypeProductImage: KindImage,
		media.MediaTypeSticker:      KindImage,
		media.MediaTypeProductVideo: KindVideo,
		media.MediaTypeVslVideo:     KindVideo,
		media.MediaTypeAudio:        KindAudio,
		media.MediaTypeDocumentPdf:  KindDocument,
		media.MediaTypeDocument:     KindDocument,
	}
	for in, want := range cases {
		if got := KindForMedia(in); got != want {
			t.Errorf("%s = %s, want %s", in, got, want)
		}
	}
}
