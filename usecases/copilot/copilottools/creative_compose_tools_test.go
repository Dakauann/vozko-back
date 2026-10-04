package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/creativecompose"
	"vozko/domain/media"
)

const (
	photoID = "4f6b1e0a-3c2d-4e5f-8a9b-1c2d3e4f5a6b"
	logoID  = "7a8b9c0d-1e2f-4a3b-9c4d-5e6f7a8b9c0d"
)

type stubComposer struct {
	layout creativecompose.Layout
	err    error
}

func (s *stubComposer) Compose(_ context.Context, workspaceID string, layout creativecompose.Layout) (*media.Media, error) {
	s.layout = layout
	if s.err != nil {
		return nil, s.err
	}
	return &media.Media{ID: "media-9", WorkspaceID: workspaceID, URL: "https://cdn/criativo.png"}, nil
}

func TestComposeCreativeShowsTheImageInTheChatWithoutAskingForApproval(t *testing.T) {
	composer := &stubComposer{}
	tool := NewComposeCreativeTool(composer)
	if tool.Meta().Mutating {
		t.Fatal("composing costs nothing and publishes nothing; it must not wait for approval")
	}
	result := tool.Execute(context.Background(), adContext, map[string]interface{}{
		"template": "card", "headline": "Todos os canais", "highlight": "numa caixa só", "image_media_id": photoID, "logo_media_id": logoID,
		"callouts": []interface{}{"A IA passa para a equipe"}, "call_to_action": "Fale com a gente no WhatsApp", "whatsapp_icon": true,
	})
	if result.Status != copilot.StatusOK || result.Image == nil || result.Image.MediaID != "media-9" || result.Data.(map[string]interface{})["media_id"] != "media-9" {
		t.Fatalf("result %+v", result)
	}
	l := composer.layout
	if l.Template != creativecompose.TemplateCard || l.ImageMediaID != photoID || l.LogoMediaID != logoID || !l.WhatsAppIcon || len(l.Callouts) != 1 {
		t.Fatalf("layout %+v", l)
	}
}

func TestComposeCreativeExplainsWhatToFix(t *testing.T) {
	composer := &stubComposer{err: &creativecompose.ValidationError{Issues: []creativecompose.FieldIssue{{Field: "subline", Code: creativecompose.CodeDash}}}}
	result := NewComposeCreativeTool(composer).Execute(context.Background(), adContext, map[string]interface{}{"template": "feed", "headline": "x", "image_media_id": photoID})
	if result.Status != copilot.StatusError || !strings.Contains(result.Message, "subline") {
		t.Fatalf("result %+v", result)
	}
}

func TestComposeCreativeRefusesAnInventedMediaID(t *testing.T) {
	err := decodeArgs(map[string]interface{}{"template": "feed", "headline": "x", "image_media_id": "inventado"}, &composeCreativeArgs{})
	if !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}
