package emaildesigntools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/screencall"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

const (
	emailBlind       = "o modelo desta conversa não enxerga imagens, então nada foi visto; diga isso ao usuário sem descrever o e-mail e sugira trocar, no seletor de modelos, para um modelo com visão"
	emailNoCapture   = "o editor não devolveu a captura; tente de novo"
	emailNoPreview   = "o e-mail tem problemas que impedem a prévia, então nada foi capturado: %s. Corrija com email_edit e olhe de novo"
	emailFocusAbsent = "focus_id %q não é um bloco deste e-mail; confira os ids com email_read"
	emailRenderBroke = "a prévia do e-mail falhou; tente de novo"

	lookBoth  = "both"
	lookLight = "light"
)

type emailLookArgs struct {
	Device  string `json:"device,omitempty" enum:"both,desktop,mobile" desc:"both (padrão): computador (600 px) e celular (375 px) lado a lado; desktop ou mobile mostra um só, maior"`
	Scheme  string `json:"scheme,omitempty" enum:"light,dark" desc:"dark: o e-mail sobre o fundo escuro de um app de e-mail em modo escuro, como o Fundo escuro da prévia"`
	FocusID string `json:"focus_id,omitempty" desc:"aproxima um bloco (id de email_read), com margem, para ler detalhes"`
	Marks   bool   `json:"marks,omitempty" desc:"numera e contorna cada bloco e liga cada número ao id na resposta"`
}

type lookPayload struct {
	HTML    string `json:"html"`
	Device  string `json:"device"`
	Scheme  string `json:"scheme"`
	FocusID string `json:"focus_id,omitempty"`
	Marks   bool   `json:"marks,omitempty"`
}

type emailLookTool struct {
	designScope
	deps Deps
}

func NewEmailLookTool(deps Deps) copilot.Tool {
	return &emailLookTool{deps: deps}
}

func (t *emailLookTool) Meta() copilot.Meta { return readMeta() }

func (t *emailLookTool) Definition() tools.Definition {
	return copilottools.Definition("email_look",
		"Mostra para você uma imagem do e-mail aberto do jeito que ele sai para o destinatário: o HTML real do envio, no computador e no celular, "+
			"com as imagens da biblioteca carregadas e personalização de exemplo. E-mails longos chegam cortados em faixas lado a lado, na ordem, para o texto continuar legível; "+
			"para ler detalhes, aproxime um bloco com focus_id. A resposta traz images (cada imagem, se apareceu, o tamanho original e a largura em que aparece) e image_checks "+
			"(resolução baixa para telas densas, arquivo pesado). Imagens de fora da biblioteca aparecem como um quadro cinza. Use para conferir o resultado antes de dizer que terminou.",
		emailLookArgs{})
}

func (a emailLookArgs) payload(page string) lookPayload {
	device, scheme := a.Device, a.Scheme
	if device == "" {
		device = lookBoth
	}
	if scheme == "" {
		scheme = lookLight
	}
	return lookPayload{HTML: page, Device: device, Scheme: scheme, FocusID: strings.TrimSpace(a.FocusID), Marks: a.Marks}
}

func (t *emailLookTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a emailLookArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return failure(err.Error())
	}
	if !cc.SeesImages {
		return failure(emailBlind)
	}
	if _, err := t.deps.openDesign(ctx, cc); err != nil {
		return failure(err.Error())
	}
	live, err := readLive(ctx, cc)
	if err != nil {
		return screencall.Result(cc, err)
	}
	payload := a.payload("")
	if _, found := emaildesign.FindBlock(live.Document.Blocks, payload.FocusID); payload.FocusID != "" && !found {
		return failure(fmt.Sprintf(emailFocusAbsent, payload.FocusID))
	}
	preview, err := t.deps.Designs.Render(emailcampaign_usecase.PreviewInput{Document: live.Document, Annotate: true, Draft: true})
	if err != nil {
		return failure(previewFailure(err))
	}
	page, reports := t.deps.inlineImages(ctx, live.Document, preview.HTML, maxInlinedBytes)
	payload.HTML = page
	reply, err := screencall.Run(ctx, cc, copilot.ScreenLook, payload, editorWords)
	if err != nil {
		return screencall.Result(cc, err)
	}
	if len(reply.Images) != 1 {
		return failure(emailNoCapture)
	}
	data := map[string]interface{}{}
	if len(reply.Data) > 0 && json.Unmarshal(reply.Data, &data) != nil {
		return failure(emailNoCapture)
	}
	data["images"] = reports
	data["image_checks"] = imageChecks(reports)
	return copilot.Result{Status: copilot.StatusOK, Data: withOpenGaps(data, live.Document), Images: reply.Images}
}

func previewFailure(err error) string {
	var invalid *emaildesign.ValidationError
	if errors.As(err, &invalid) {
		return fmt.Sprintf(emailNoPreview, describeIssues(invalid.Issues))
	}
	return emailRenderBroke
}
