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

type stubComposer struct {
	layout creativecompose.Layout
	err    error
}

func (s *stubComposer) Captures() []creativecompose.LibraryCapture {
	return []creativecompose.LibraryCapture{{ID: "inbox", Kind: creativecompose.LibraryScreen, Feature: "Atendimento", Description: "Caixa de entrada"}}
}

func (s *stubComposer) Compose(_ context.Context, workspaceID string, layout creativecompose.Layout) (*media.Media, error) {
	s.layout = layout
	if s.err != nil {
		return nil, s.err
	}
	return &media.Media{ID: "media-9", WorkspaceID: workspaceID, URL: "https://cdn/criativo.png"}, nil
}

func creativeTool(composer *stubComposer, name string) copilot.Tool {
	for _, tool := range CreativeTools(composer) {
		if tool.Definition().Name == name {
			return tool
		}
	}
	return nil
}

func TestComposeCreativeShowsTheImageInTheChatWithoutAskingForApproval(t *testing.T) {
	composer := &stubComposer{}
	tool := creativeTool(composer, "compose_creative")
	if tool.Meta().Mutating {
		t.Fatal("composing costs nothing and publishes nothing; it must not wait for approval")
	}
	result := tool.Execute(context.Background(), adContext, map[string]interface{}{
		"template": "card", "headline": "Todos os canais", "highlight": "numa caixa só", "capture": "inbox", "logo": "vozko-logo",
		"callouts": []interface{}{"A IA passa para a equipe"}, "call_to_action": "Fale com a gente no WhatsApp", "whatsapp_icon": true,
	})
	if result.Status != copilot.StatusOK || result.Image == nil || result.Image.MediaID != "media-9" || result.Data.(map[string]interface{})["media_id"] != "media-9" {
		t.Fatalf("result %+v", result)
	}
	l := composer.layout
	if l.Template != creativecompose.TemplateCard || l.Capture.LibraryID != "inbox" || l.Logo.LibraryID != "vozko-logo" || !l.WhatsAppIcon || len(l.Callouts) != 1 {
		t.Fatalf("layout %+v", l)
	}
}

func TestComposeCreativeExplainsWhatToFix(t *testing.T) {
	composer := &stubComposer{err: &creativecompose.ValidationError{Issues: []creativecompose.FieldIssue{{Field: "subline", Code: creativecompose.CodeDash}}}}
	result := creativeTool(composer, "compose_creative").Execute(context.Background(), adContext, map[string]interface{}{"template": "feed", "headline": "x", "capture": "inbox"})
	if result.Status != copilot.StatusError || !strings.Contains(result.Message, "subline") {
		t.Fatalf("result %+v", result)
	}
}

func TestComposeCreativeRefusesAnInventedMediaID(t *testing.T) {
	err := decodeArgs(map[string]interface{}{"template": "feed", "headline": "x", "capture_media_id": "inventado"}, &composeCreativeArgs{})
	if !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}

func TestListCreativeCapturesDescribesEachScreen(t *testing.T) {
	result := creativeTool(&stubComposer{}, "list_creative_captures").Execute(context.Background(), adContext, nil)
	captures := result.Data.(map[string]interface{})["captures"].([]map[string]string)
	if len(captures) != 1 || captures[0]["capture"] != "inbox" || captures[0]["shows"] != "Caixa de entrada" {
		t.Fatalf("captures %+v", captures)
	}
}
