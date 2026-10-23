package emaildesigntools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/media"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/mediatools"
	"vozko/usecases/copilot/copilottools/screencall"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

type Designs interface {
	GetDesign(ctx context.Context, a emailcampaign_usecase.Actor, designID string) (*emailcampaign.Design, error)
	CreateDesign(ctx context.Context, a emailcampaign_usecase.Actor, in emailcampaign.DesignInput) (*emailcampaign.Design, error)
	Render(in emailcampaign_usecase.PreviewInput) (emailcampaign_usecase.Preview, error)
}

type Library interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type Deps struct {
	Designs   Designs
	Campaigns Campaigns
	Library   Library
	Files     media.FileReader
	Media     mediatools.MediaGeneration
	IDs       emaildesign.IDSource
}

const (
	emailNotHere       = "esta ferramenta só funciona com um e-mail aberto no editor: um design em Modelos ou o conteúdo de uma campanha"
	emailNoEditor      = "o editor de e-mail não está ligado a esta conversa; peça ao usuário para abrir a Elo de dentro do editor do e-mail"
	emailDesignGone    = "o design aberto não foi encontrado neste workspace; peça ao usuário para reabrir o design"
	emailEditorAwaited = "A aba do e-mail deve estar em segundo plano ou adormecida. Não peça ao usuário para avisar nem para deixar a aba aberta: quando o editor voltar a responder, o app avisa você nesta resposta ou retoma a conversa sozinho. Não repita o pedido agora; siga com o que não depende do editor ou encerre dizendo o que está pronto e o que falta."
	emailUnreadable    = "o editor devolveu um e-mail que não pôde ser lido; tente de novo"
)

var editorWords = screencall.Words{
	Silent:      "o editor não respondeu a tempo e nada mudou no e-mail. " + emailEditorAwaited,
	Expired:     "o editor não respondeu a tempo e nada deste pedido foi aplicado: ele expirou e não entra depois, nem quando a aba voltar. " + emailEditorAwaited,
	Unconfirmed: "o editor começou a aplicar este pedido e não confirmou o resultado a tempo; quando ele voltar a responder, confira com email_read antes de repetir qualquer coisa. " + emailEditorAwaited,
	Absent:      "nenhuma aba com este e-mail aberto respondeu e nada foi aplicado; se o usuário está com o e-mail aberto, a conexão da aba caiu por um instante e ela se reconecta sozinha: tente de novo uma vez antes de pedir algo ao usuário",
	Refused:     "o editor recusou o pedido",
}

type designScope struct{}

func (designScope) OfferedOn(view copilot.View) bool { return view.OnEmailDocument() }

func readMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionRead}
}

func editMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionUpdate}
}

func failure(message string) copilot.Result {
	return copilot.Result{Status: copilot.StatusError, Message: message}
}

func actorOf(cc copilot.Context) emailcampaign_usecase.Actor {
	return copilottools.ViewerOf(cc)
}

func (d Deps) ids() emaildesign.IDSource {
	if d.IDs != nil {
		return d.IDs
	}
	return emaildesign.RandomIDs()
}

type liveDesign struct {
	Document emaildesign.Document
	Selected string
	Name     string
	Envelope *envelope
}

type liveReply struct {
	Document  json.RawMessage `json:"document"`
	Selected  string          `json:"selected,omitempty"`
	Name      string          `json:"name,omitempty"`
	Subject   *string         `json:"subject,omitempty"`
	Preheader *string         `json:"preheader,omitempty"`
}

func envelopeOf(subject, preheader *string, wanted bool) (*envelope, bool) {
	if !wanted {
		return nil, subject == nil && preheader == nil
	}
	if subject == nil || preheader == nil {
		return nil, false
	}
	return &envelope{Subject: *subject, Preheader: *preheader}, true
}

func decodeLive(data []byte, wantEnvelope bool) (liveDesign, error) {
	var reply liveReply
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reply); err != nil || len(reply.Document) == 0 {
		return liveDesign{}, errors.New(emailUnreadable)
	}
	doc, err := emaildesign.Decode(reply.Document)
	if err != nil {
		return liveDesign{}, errors.New(emailUnreadable)
	}
	env, ok := envelopeOf(reply.Subject, reply.Preheader, wantEnvelope)
	if !ok {
		return liveDesign{}, errors.New(emailUnreadable)
	}
	return liveDesign{Document: doc, Selected: strings.TrimSpace(reply.Selected), Name: strings.TrimSpace(reply.Name), Envelope: env}, nil
}

func readLive(ctx context.Context, cc copilot.Context) (liveDesign, error) {
	reply, err := screencall.Run(ctx, cc, copilot.ScreenRead, map[string]string{}, editorWords)
	if err != nil {
		return liveDesign{}, err
	}
	return decodeLive(reply.Data, cc.View.OnEmailCampaign())
}

func blockView(blocks []emaildesign.Block) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Fields())
	}
	return out
}

func withFacts(body, facts map[string]interface{}) map[string]interface{} {
	for key, value := range facts {
		body[key] = value
	}
	return body
}

func EmailDesignTools(deps Deps) []copilot.Tool {
	return []copilot.Tool{
		NewEmailReadTool(deps),
		NewEmailEditTool(deps),
		NewEmailLookTool(deps),
		NewEmailGenerateImageTool(deps),
		NewNewEmailDesignTool(deps),
	}
}
