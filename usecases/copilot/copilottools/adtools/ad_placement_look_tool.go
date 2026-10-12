package adtools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
	"vozko/usecases/copilot/copilottools"
)

type AdPlacementLook interface {
	Look(ctx context.Context, in adsuc.PlacementLookInput) (*adsuc.PlacementLook, error)
}

type adPlacementLookArgs struct {
	MediaID    string   `json:"media_id" req:"true" id:"true" desc:"media_id da imagem: de generate_image, de um anexo do usuário ou de studio_export"`
	Placements []string `json:"placements" desc:"os posicionamentos do anúncio, como em create_ad: vazio ou automatic, ou plataformas entre facebook, instagram, messenger, audience_network e threads"`
}

const (
	placementLookBlind = "este modelo não vê imagens: nenhuma prévia foi enviada e você não olhou o criativo. Decida só pelos números e diga ao usuário que a conferência foi numérica"
	placementLookKey   = "uma imagem por item de frames, na mesma ordem (preview). Vermelho é a interface que cobre o anúncio; o essencial fica dentro do contorno amarelo. detail é a parte com bordas fortes (texto, logotipo, rosto, contornos), medida nos pixels; coordenadas de 0 a 1 do quadro"
	percent            = 100
)

var frameTitles = map[string]string{
	"feed":             "Feeds",
	"vertical":         "Stories e Reels (9:16)",
	"square":           "Marketplace (1:1)",
	"right_column":     "Coluna da direita (1:1, computador)",
	"search":           "Resultados da pesquisa",
	"messenger":        "Mensagens patrocinadas do Messenger",
	"audience_network": "Audience Network (apps e sites)",
	"thumbnail":        "Miniaturas (notificações e sobreposição no Reels)",
}

var positionNames = map[advertising.PlacementPosition]string{
	{Platform: "facebook", Position: "feed"}:                   "Feed do Facebook",
	{Platform: "facebook", Position: "profile_feed"}:           "Feed do perfil do Facebook",
	{Platform: "facebook", Position: "right_hand_column"}:      "Coluna da direita do Facebook",
	{Platform: "facebook", Position: "marketplace"}:            "Marketplace do Facebook",
	{Platform: "facebook", Position: "story"}:                  "Stories do Facebook",
	{Platform: "facebook", Position: "search"}:                 "Pesquisa do Facebook",
	{Platform: "facebook", Position: "instream_video"}:         "Vídeos in-stream do Facebook",
	{Platform: "facebook", Position: "facebook_reels"}:         "Reels do Facebook",
	{Platform: "facebook", Position: "facebook_reels_overlay"}: "Sobreposição no Reels do Facebook",
	{Platform: "facebook", Position: "notification"}:           "Notificações do Facebook",
	{Platform: "instagram", Position: "stream"}:                "Feed do Instagram",
	{Platform: "instagram", Position: "story"}:                 "Stories do Instagram",
	{Platform: "instagram", Position: "explore_home"}:          "Explorar do Instagram",
	{Platform: "instagram", Position: "reels"}:                 "Reels do Instagram",
	{Platform: "instagram", Position: "profile_feed"}:          "Feed do perfil do Instagram",
	{Platform: "instagram", Position: "ig_search"}:             "Pesquisa do Instagram",
	{Platform: "instagram", Position: "profile_reels"}:         "Reels do perfil do Instagram",
	{Platform: "messenger", Position: "sponsored_messages"}:    "Mensagens patrocinadas do Messenger",
	{Platform: "audience_network", Position: "classic"}:        "Audience Network",
	{Platform: "audience_network", Position: "rewarded_video"}: "Vídeo com incentivo do Audience Network",
	{Platform: "threads", Position: "threads_stream"}:          "Feed do Threads",
}

var placementIssueTexts = map[string]func(adsuc.PlacementLookFrame) string{
	advertising.IssueDetailCut: func(f adsuc.PlacementLookFrame) string {
		return fmt.Sprintf("o corte tira %d%% do detalhe da imagem", share(f.Detail.Cut))
	},
	advertising.IssueDetailUnderUI: func(f adsuc.PlacementLookFrame) string {
		return fmt.Sprintf("%d%% do detalhe visível fica embaixo da interface", share(f.Detail.UnderUI))
	},
	advertising.IssueBackground: func(f adsuc.PlacementLookFrame) string {
		return fmt.Sprintf("%d%% do quadro vira fundo preenchido pela Meta", share(f.Layout.Background))
	},
	advertising.IssueBelowMinimum: func(f adsuc.PlacementLookFrame) string {
		return fmt.Sprintf("a parte mostrada tem %s, abaixo do mínimo de %s", sizeText(f.Layout.Visible), sizeText(f.Planned.Frame.Minimum))
	},
}

type adPlacementLookTool struct {
	studioAdRoute
	deps AdsDeps
}

func NewAdPlacementLookTool(deps AdsDeps) copilot.Tool { return &adPlacementLookTool{deps: deps} }

func (t *adPlacementLookTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *adPlacementLookTool) Definition() tools.Definition {
	return copilottools.Definition("ad_placement_look",
		"Mostra como uma imagem aparece em cada posicionamento do anúncio: uma prévia por grupo de posicionamentos, com o corte da Meta e a "+
			"interface por cima, e números de corte, fundo, resolução, detalhe cortado ou coberto e altura mínima de texto legível. "+
			"Use antes de create_ad, save_ad_draft ou update_ad_draft com uma imagem gerada, editada ou enviada. Só informa; não bloqueia nem cobra.",
		adPlacementLookArgs{})
}

func (t *adPlacementLookTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adPlacementLookArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	placements, err := parsePlacements(a.Placements)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	look, err := t.deps.Placements.Look(ctx, adsuc.PlacementLookInput{
		WorkspaceID: cc.WorkspaceID, MediaID: a.MediaID, Placements: placements, Panels: cc.SeesImages, Labels: panelLabels,
	})
	if err != nil {
		return placementLookFailure(a.MediaID, err)
	}
	data := map[string]interface{}{"image": sizeText(look.Size), "seen": cc.SeesImages, "frames": lookFrames(look), "how_to_read": placementLookKey}
	if !cc.SeesImages {
		data["note"] = placementLookBlind
	}
	if len(look.VideoOnly) > 0 {
		data["video_only"] = positionsText(look.VideoOnly) + ": só vídeo, esta imagem não aparece ali"
	}
	res := copilot.Result{Status: copilot.StatusOK, Data: data}
	if cc.SeesImages {
		for _, f := range look.Frames {
			res.Images = append(res.Images, f.Panel)
		}
	}
	return res
}

func placementLookFailure(mediaID string, err error) copilot.Result {
	var invalid *advertising.ValidationError
	var message string
	switch {
	case errors.Is(err, media.ErrMediaNotFound), errors.Is(err, advertising.ErrMediaNotImage):
		message = fmt.Sprintf("media_id %q não é uma imagem da biblioteca deste workspace; use o media_id exato de generate_image, de um anexo ou de studio_export (imagens que já estão na Meta não têm prévia aqui)", mediaID)
	case errors.Is(err, adsuc.ErrPlacementImageUnreadable):
		message = "a imagem não pôde ser lida para a prévia e nada foi conferido; gere ou exporte de novo em PNG ou JPEG"
	case errors.Is(err, adsuc.ErrNoImagePlacement):
		message = "nenhum desses posicionamentos mostra imagem (só vídeo); escolha outros ou use um vídeo"
	case errors.As(err, &invalid):
		message = adsMessage(err)
	default:
		log.Printf("[copilot] ad_placement_look failed: %v", err)
		message = "a imagem não pôde ser baixada agora e nada foi conferido; tente de novo em instantes"
	}
	return copilot.Result{Status: copilot.StatusError, Message: message}
}

func panelLabels(frameID string) adsuc.PanelLabels {
	labels := adsuc.PanelLabels{Title: frameTitles[frameID]}
	if frame, ok := advertising.PlacementFrameByID(frameID); ok && frame.Safe != nil {
		labels.Top, labels.Bottom = "perfil e controles", "legenda, botão e ações"
	}
	return labels
}

func lookFrames(look *adsuc.PlacementLook) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(look.Frames))
	for i, f := range look.Frames {
		row := map[string]interface{}{
			"preview": i + 1, "frame": frameTitles[f.Planned.Frame.ID], "placements": positionsText(f.Planned.Positions),
			"shown": shownText(f.Layout), "visible_px": sizeText(f.Layout.Visible), "recommended_px": sizeText(f.Planned.Frame.Recommended),
			"min_text_px": f.Layout.MinTextPx,
		}
		if f.Planned.Frame.Safe != nil {
			row["safe_area"] = rectText(f.Layout.Safe)
			row["detail_under_ui"] = f.Detail.UnderUI
		}
		if f.Detail.Cut > 0 {
			row["detail_cut"] = f.Detail.Cut
		}
		if f.Detail.Bounds != nil {
			row["detail_bounds"] = rectText(*f.Detail.Bounds)
		}
		warnings := make([]string, 0, len(f.Issues))
		for _, code := range f.Issues {
			warnings = append(warnings, placementIssueTexts[code](f))
		}
		if len(warnings) > 0 {
			row["warnings"] = warnings
		}
		out = append(out, row)
	}
	return out
}

func share(v float64) int { return int(math.Round(v * percent)) }

func sizeText(s advertising.PixelSize) string {
	if s.Height == 0 {
		return fmt.Sprintf("%d de largura", s.Width)
	}
	return fmt.Sprintf("%dx%d", s.Width, s.Height)
}

func rectText(r advertising.FrameRect) string {
	return fmt.Sprintf("x %.2f a %.2f, y %.2f a %.2f", r.X0, r.X1, r.Y0, r.Y1)
}

func ratioText(r float64) string {
	for _, known := range []struct {
		ratio float64
		name  string
	}{{0.5625, "9:16"}, {0.8, "4:5"}, {1, "1:1"}, {1.91, "1,91:1"}} {
		if math.Abs(r-known.ratio) < 0.005 {
			return known.name
		}
	}
	return strings.Replace(fmt.Sprintf("%.2f:1", r), ".", ",", 1)
}

func shownText(l advertising.FrameLayout) string {
	switch {
	case l.Background > 0:
		return fmt.Sprintf("imagem inteira na largura de um quadro 9:16; %d%% do quadro vira fundo preenchido pela Meta", share(l.Background))
	case l.CropLoss > 0:
		return fmt.Sprintf("corte centralizado em %s, que tira %d%% da imagem", ratioText(l.Ratio), share(l.CropLoss))
	}
	return "imagem inteira, sem corte, em " + ratioText(l.Ratio)
}

func positionsText(positions []advertising.PlacementPosition) string {
	names := make([]string, 0, len(positions))
	for _, p := range positions {
		names = append(names, positionNames[p])
	}
	return strings.Join(names, ", ")
}
