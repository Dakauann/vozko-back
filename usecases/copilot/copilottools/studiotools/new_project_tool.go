package studiotools

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/studio"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/screencall"
)

type StudioMediaLibrary interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type newStudioProjectArgs struct {
	Kind       string   `json:"kind" req:"true" enum:"image,video" desc:"image para arte com pranchetas, video para vídeo"`
	MediaIDs   []string `json:"media_ids" id:"true" desc:"opcional: media_id de imagens (e, no vídeo, vídeos e áudios) de generate_image, anexos ou da biblioteca; a primeira imagem entra em cada prancheta"`
	Sizes      []string `json:"sizes" desc:"opcional. Imagem: uma prancheta por item, LARGURAxALTURA de 100 a 4096 px ou o nome de um grupo de posicionamentos de ad_placement_look (feed, vertical, square, right_column, search, messenger, audience_network, thumbnail). Vídeo: um item entre square, portrait, story e landscape. Vazio segue a primeira mídia ou o padrão de um projeto novo"`
	DurationMs int      `json:"duration_ms" desc:"opcional, só vídeo: quanto tempo cada imagem fica na tela, em ms"`
	Name       string   `json:"name" desc:"opcional: nome do projeto"`
}

const (
	newProjectOpened = "projeto criado e aberto: o editor dele já está na tela do usuário e as ferramentas studio_ entram na sua próxima chamada. " +
		"Continue o trabalho agora, começando por studio_read; não peça que ele abra nem que confirme nada"
	newProjectWaiting = "projeto criado, mas o editor continua fechado: ele abre quando o usuário tocar em Abrir no Estúdio no cartão. Diga isso uma vez e não diga que abriu. " +
		"Quando ele abrir, o app avisa você e as ferramentas studio_ chegam; nunca peça que ele avise que abriu"
	newProjectNoApp       = "nenhuma aba do app está ligada a esta conversa agora, então nada foi criado; tente de novo em instantes"
	newProjectSilent      = "o app não confirmou a criação do projeto a tempo e nada foi aberto; tente de novo uma vez"
	newProjectExpired     = "o app não respondeu a tempo e nada foi criado: o pedido expirou e não entra depois; tente de novo uma vez"
	newProjectUnconfirmed = "o app começou a criar o projeto e não confirmou a tempo; não peça outro agora para não duplicar. Diga ao usuário que a criação ficou sem confirmação e que, se o projeto foi criado, ele está na lista do Estúdio"
	newProjectAbsent      = "nenhuma aba com esta conversa aberta conseguiu criar o projeto e nada foi criado; tente de novo uma vez antes de pedir algo ao usuário"
	newProjectRefuse      = "o app não criou o projeto"
	newProjectBroken      = "o app devolveu um projeto que não pôde ser lido; nada foi aberto"

	stateOpened   = "opened"
	stateWaiting  = "waiting"
	sizeSeparator = "x"
)

var creationWords = screencall.Words{Silent: newProjectSilent, Expired: newProjectExpired, Unconfirmed: newProjectUnconfirmed, Absent: newProjectAbsent, Refused: newProjectRefuse}

type newStudioProjectTool struct{ deps StudioDeps }

func NewNewStudioProjectTool(deps StudioDeps) copilot.Tool { return &newStudioProjectTool{deps: deps} }

func (t *newStudioProjectTool) OfferedOn(view copilot.View) bool { return !view.Focused() }

func (t *newStudioProjectTool) Meta() copilot.Meta { return studioEditMeta() }

func (t *newStudioProjectTool) Definition() tools.Definition {
	return copilottools.Definition("new_studio_project",
		"Cria um projeto do Estúdio, vazio ou com mídias da biblioteca, e mostra um cartão que leva o usuário direto ao editor desse projeto "+
			"(nunca ao menu do Estúdio). O retorno diz o estado real: opened (o editor já abriu na tela dele e as ferramentas do Estúdio entram na "+
			"sua próxima chamada) ou waiting (criado, esperando ele abrir pelo cartão; o app avisa quando ele abrir). Imagem: uma prancheta por "+
			"tamanho, por exemplo uma por posicionamento do anúncio. Lá ele edita à mão e você edita junto com as ferramentas do Estúdio; depois o "+
			"criativo volta ao anúncio com studio_export.",
		newStudioProjectArgs{})
}

func (t *newStudioProjectTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a newStudioProjectArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	order, err := t.order(cc.WorkspaceID, a)
	if err != nil {
		return studioFailure(err.Error())
	}
	card, err := copilot.NewStudioProjectCard(order)
	if err != nil {
		return studioFailure(fmt.Sprintf("%v: confira kind, sizes, duration_ms e as mídias", copilottools.ErrInvalidArgs))
	}
	if cc.Screen == nil {
		return studioFailure(newProjectNoApp)
	}
	reply, err := screencall.Run(ctx, cc, copilot.ScreenCreateProject, order, creationWords)
	if err != nil {
		return studioFailure(err.Error())
	}
	opening, err := copilot.OpeningOf(order, reply.Data)
	if err != nil {
		return studioFailure(newProjectBroken)
	}
	card.StudioProject.ProjectID = opening.View.ProjectID
	return copilot.Result{Status: copilot.StatusOK, Card: card, Opening: &opening, Data: openingData(opening)}
}

func openingData(opening copilot.ProjectOpening) map[string]interface{} {
	state, next := stateWaiting, newProjectWaiting
	if opening.Opened {
		state, next = stateOpened, newProjectOpened
	}
	return map[string]interface{}{"state": state, "project_id": opening.View.ProjectID, "kind": string(opening.View.ProjectKind), "next": next}
}

func (t *newStudioProjectTool) order(workspaceID string, a newStudioProjectArgs) (copilot.StudioProjectOrder, error) {
	order := copilot.StudioProjectOrder{Kind: a.Kind, DurationMs: a.DurationMs}
	if strings.TrimSpace(a.Name) != "" {
		name, err := studio.CleanProjectName(a.Name)
		if err != nil {
			return order, fmt.Errorf("%w: name vai até %d caracteres", copilottools.ErrInvalidArgs, studio.MaxNameRunes)
		}
		order.Name = name
	}
	if err := setSizes(&order, a.Sizes); err != nil {
		return order, err
	}
	if a.DurationMs != 0 && studio.Kind(a.Kind) != studio.KindVideo {
		return order, fmt.Errorf("%w: duration_ms só vale para video", copilottools.ErrInvalidArgs)
	}
	if a.DurationMs != 0 && (a.DurationMs < copilot.MinStudioStillMs || a.DurationMs > studio.MaxVideoMS) {
		return order, fmt.Errorf("%w: duration_ms vai de %d a %d", copilottools.ErrInvalidArgs, copilot.MinStudioStillMs, studio.MaxVideoMS)
	}
	for _, id := range a.MediaIDs {
		m, err := t.media(workspaceID, strings.TrimSpace(id), studio.Kind(a.Kind))
		if err != nil {
			return order, err
		}
		order.Media = append(order.Media, m)
	}
	return order, nil
}

func setSizes(order *copilot.StudioProjectOrder, raw []string) error {
	if studio.Kind(order.Kind) == studio.KindVideo {
		switch {
		case len(raw) > 1:
			return fmt.Errorf("%w: um vídeo tem um só formato em sizes", copilottools.ErrInvalidArgs)
		case len(raw) == 1 && !copilot.ValidStudioAspect(strings.TrimSpace(raw[0])):
			return fmt.Errorf("%w: no vídeo, sizes é square, portrait, story ou landscape", copilottools.ErrInvalidArgs)
		case len(raw) == 1:
			order.Aspect = strings.TrimSpace(raw[0])
		}
		return nil
	}
	for _, item := range raw {
		size, err := artboardSize(item)
		if err != nil {
			return err
		}
		if !slices.Contains(order.Sizes, size) {
			order.Sizes = append(order.Sizes, size)
		}
	}
	return nil
}

func artboardSize(raw string) (copilot.CanvasSize, error) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if frame, ok := advertising.PlacementFrameByID(clean); ok {
		return copilot.CanvasSize{Width: frame.Recommended.Width, Height: frame.Recommended.Height}, nil
	}
	w, h, found := strings.Cut(clean, sizeSeparator)
	width, werr := strconv.Atoi(strings.TrimSpace(w))
	height, herr := strconv.Atoi(strings.TrimSpace(h))
	size := copilot.CanvasSize{Width: width, Height: height}
	if !found || werr != nil || herr != nil || !size.Valid() {
		return size, fmt.Errorf("%w: sizes %q precisa ser LARGURAxALTURA de %d a %d px ou um grupo de posicionamentos (feed, vertical, square...)",
			copilottools.ErrInvalidArgs, raw, studio.MinCanvasSide, studio.MaxCanvasSide)
	}
	return size, nil
}

func studioMediaType(t media.MediaType) string {
	switch t {
	case media.MediaTypeProductImage:
		return copilot.StudioMediaImage
	case media.MediaTypeProductVideo:
		return copilot.StudioMediaVideo
	case media.MediaTypeAudio:
		return copilot.StudioMediaAudio
	}
	return ""
}

func (t *newStudioProjectTool) media(workspaceID, id string, kind studio.Kind) (copilot.StudioProjectMedia, error) {
	if t.deps.Library == nil {
		return copilot.StudioProjectMedia{}, fmt.Errorf("a biblioteca de mídias não está disponível agora; nada foi criado")
	}
	m, err := t.deps.Library.GetMedia(workspaceID, id)
	if err != nil || m == nil || strings.TrimSpace(m.URL) == "" {
		return copilot.StudioProjectMedia{}, fmt.Errorf("%w: media_id %q não está na biblioteca deste workspace", copilottools.ErrInvalidArgs, id)
	}
	kindOf := studioMediaType(m.Type)
	if kindOf == "" || (kind == studio.KindImage && kindOf != copilot.StudioMediaImage) {
		return copilot.StudioProjectMedia{}, fmt.Errorf("%w: media_id %q não serve para um projeto de %s (imagem aceita imagens; vídeo aceita imagens, vídeos e áudios)", copilottools.ErrInvalidArgs, id, kind)
	}
	return copilot.StudioProjectMedia{MediaID: m.ID, URL: m.URL, Type: kindOf}, nil
}
