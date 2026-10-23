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
	Designs Designs
	Library Library
	Files   media.FileReader
	Media   mediatools.MediaGeneration
	IDs     emaildesign.IDSource
}

const (
	emailNotHere       = "esta ferramenta só funciona com um design de e-mail aberto no editor"
	emailNoEditor      = "o editor de e-mail não está ligado a esta conversa; peça ao usuário para abrir a Elo de dentro do design"
	emailDesignGone    = "o design aberto não foi encontrado neste workspace; peça ao usuário para reabrir o design"
	emailEditorAwaited = "A aba do design deve estar em segundo plano ou adormecida. Não peça ao usuário para avisar nem para deixar a aba aberta: quando o editor voltar a responder, o app avisa você nesta resposta ou retoma a conversa sozinho. Não repita o pedido agora; siga com o que não depende do editor ou encerre dizendo o que está pronto e o que falta."
	emailUnreadable    = "o editor devolveu um design que não pôde ser lido; tente de novo"
)

var editorWords = screencall.Words{
	Silent:      "o editor não respondeu a tempo e nada mudou no design. " + emailEditorAwaited,
	Expired:     "o editor não respondeu a tempo e nada deste pedido foi aplicado: ele expirou e não entra depois, nem quando a aba voltar. " + emailEditorAwaited,
	Unconfirmed: "o editor começou a aplicar este pedido e não confirmou o resultado a tempo; quando ele voltar a responder, confira com email_read antes de repetir qualquer coisa. " + emailEditorAwaited,
	Absent:      "nenhuma aba com este design aberto respondeu e nada foi aplicado; se o usuário está com o design aberto, a conexão da aba caiu por um instante e ela se reconecta sozinha: tente de novo uma vez antes de pedir algo ao usuário",
	Refused:     "o editor recusou o pedido",
}

type designScope struct{}

func (designScope) OfferedOn(view copilot.View) bool { return view.OnEmailDesign() }

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

func (d Deps) openDesign(ctx context.Context, cc copilot.Context) (*emailcampaign.Design, error) {
	if !cc.View.OnEmailDesign() {
		return nil, errors.New(emailNotHere)
	}
	if cc.Screen == nil {
		return nil, errors.New(emailNoEditor)
	}
	if d.Designs == nil {
		return nil, errors.New(emailDesignGone)
	}
	design, err := d.Designs.GetDesign(ctx, actorOf(cc), cc.View.ProjectID)
	if err != nil || design == nil {
		return nil, errors.New(emailDesignGone)
	}
	return design, nil
}

type liveDesign struct {
	Document emaildesign.Document
	Selected string
	Name     string
}

type liveReply struct {
	Document json.RawMessage `json:"document"`
	Selected string          `json:"selected,omitempty"`
	Name     string          `json:"name,omitempty"`
}

func decodeLive(data []byte) (liveDesign, error) {
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
	return liveDesign{Document: doc, Selected: strings.TrimSpace(reply.Selected), Name: strings.TrimSpace(reply.Name)}, nil
}

func readLive(ctx context.Context, cc copilot.Context) (liveDesign, error) {
	reply, err := screencall.Run(ctx, cc, copilot.ScreenRead, map[string]string{}, editorWords)
	if err != nil {
		return liveDesign{}, err
	}
	return decodeLive(reply.Data)
}

func blockView(blocks []emaildesign.Block) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Fields())
	}
	return out
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
