package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/creativecompose"
	"vozko/domain/media"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type CreativeComposer interface {
	Compose(ctx context.Context, workspaceID string, layout creativecompose.Layout) (*media.Media, error)
}

func NewComposeCreativeTool(composer CreativeComposer) copilot.Tool {
	return &composeCreativeTool{composer: composer}
}

type composeCreativeArgs struct {
	Template     string   `json:"template" req:"true" enum:"feed,card,story" desc:"feed (4:5, uma imagem no feed), card (1:1, cartão de carrossel) ou story (9:16, stories e reels)"`
	Eyebrow      string   `json:"eyebrow" desc:"rótulo curto no topo à direita, ex.: o recurso do cartão (até 28 caracteres)"`
	Headline     string   `json:"headline" req:"true" desc:"título em branco (até 32 caracteres)"`
	Highlight    string   `json:"highlight" desc:"segunda linha do título, em verde (até 32 caracteres)"`
	Subline      string   `json:"subline" desc:"frase de apoio (até 120 caracteres)"`
	ImageMediaID string   `json:"image_media_id" req:"true" id:"true" desc:"media_id da imagem do usuário que aparece no anúncio (anexo ou biblioteca): foto do produto, do serviço, do espaço ou print do app ou site dele"`
	LogoMediaID  string   `json:"logo_media_id" id:"true" desc:"media_id do logo do usuário, anexado na conversa; vazio deixa sem logo"`
	Callouts     []string `json:"callouts" desc:"até 3 chamadas curtas sobre a tela, cada uma dizendo o que ela mostra de verdade (até 44 caracteres)"`
	CallToAction string   `json:"call_to_action" desc:"texto do botão desenhado na arte, ex.: Fale com a gente no WhatsApp (até 32 caracteres)"`
	WhatsAppIcon bool     `json:"whatsapp_icon" desc:"true desenha o ícone do WhatsApp no botão, para anúncios que levam ao WhatsApp"`
	Footnote     string   `json:"footnote" desc:"texto pequeno no rodapé, ex.: o site (até 32 caracteres)"`
}

func (a composeCreativeArgs) layout() creativecompose.Layout {
	return creativecompose.Layout{
		Template: creativecompose.Template(a.Template), Eyebrow: a.Eyebrow, Headline: a.Headline, Highlight: a.Highlight, Subline: a.Subline,
		ImageMediaID: a.ImageMediaID, LogoMediaID: a.LogoMediaID,
		Callouts: a.Callouts, CallToAction: a.CallToAction, WhatsAppIcon: a.WhatsAppIcon, Footnote: a.Footnote,
	}
}

type composeCreativeTool struct{ composer CreativeComposer }

func (t *composeCreativeTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceMedia, Action: workspace.ActionCreate}
}

func (t *composeCreativeTool) Definition() tools.Definition {
	return definition("compose_creative",
		"Monta um criativo profissional a partir de uma imagem real do usuário (foto do produto, do serviço ou print do app ou site dele): "+
			"título, frase de apoio, chamadas, logo do usuário e botão sobre um modelo fixo, sem IA redesenhando a imagem, então nada é inventado. Grátis, salva na biblioteca de mídia e devolve media_id para create_ad e swap_ad_creative. "+
			"Para carrossel, monte um cartão (template card) por recurso. Nunca use travessões nos textos.",
		composeCreativeArgs{})
}

func (t *composeCreativeTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a composeCreativeArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	stored, err := t.composer.Compose(ctx, cc.WorkspaceID, a.layout())
	var invalid *creativecompose.ValidationError
	if errors.As(err, &invalid) {
		return copilot.Result{Status: copilot.StatusError, Message: composeIssues(invalid)}
	}
	if err != nil {
		log.Printf("[copilot] compose_creative failed: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "não foi possível montar o criativo agora; tente de novo"}
	}
	return copilot.Result{
		Status: copilot.StatusOK,
		Data:   map[string]interface{}{"media_id": stored.ID, "media_url": stored.URL, "template": a.Template},
		Image:  &copilot.Image{URL: stored.URL, MediaID: stored.ID, Alt: strings.TrimSpace(a.Headline + " " + a.Highlight)},
	}
}

var composeCodeTexts = map[string]string{
	creativecompose.CodeRequired: "é obrigatório",
	creativecompose.CodeTooLong:  "passou do limite de caracteres",
	creativecompose.CodeTooMany:  "tem itens demais (até 3)",
	creativecompose.CodeDash:     "tem travessão; reescreva com vírgula, dois pontos ou ponto",
	creativecompose.CodeInvalid:  "está inválido",
	creativecompose.CodeNotFound: "não foi encontrado: use o media_id de uma imagem deste workspace (anexo ou biblioteca)",
}

func composeIssues(err *creativecompose.ValidationError) string {
	parts := make([]string, 0, len(err.Issues))
	for _, issue := range err.Issues {
		parts = append(parts, fmt.Sprintf("%s %s", issue.Field, composeCodeTexts[issue.Code]))
	}
	return "ajuste o criativo: " + strings.Join(parts, "; ")
}
