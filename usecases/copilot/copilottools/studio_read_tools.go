package copilottools

import (
	"context"
	"fmt"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/tools"
)

const (
	MaxStudioLookFrames = 9
	studioBlind         = "o modelo desta conversa não enxerga imagens; peça ao usuário para trocar para um modelo com visão e tente de novo"
	studioNoCapture     = "o editor não devolveu a captura; tente de novo"
)

type studioReadArgs struct {
	Detail string `json:"detail" enum:"summary,full" desc:"summary (padrão): faixas, clipes, tempos, textos e trabalhos em andamento; full: inclui também transformações, keyframes, estilos e a biblioteca recente"`
}

type studioReadTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioReadTool(deps StudioDeps) copilot.Tool {
	return &studioReadTool{deps: deps}
}

func (t *studioReadTool) Meta() copilot.Meta { return studioReadMeta() }

func (t *studioReadTool) Definition() tools.Definition {
	return definition("studio_read",
		"Lê o projeto aberto no Estúdio como ele está agora na tela, inclusive o que o usuário mudou desde a sua última leitura: "+
			"formato, duração, faixas de baixo para cima com seus clipes (id, tipo, mídia, início, duração, recorte, textos, animações), "+
			"marcadores, posição do cursor de tempo, seleção e trabalhos na fila. Em projetos de imagem: cada prancheta (id, nome, tamanho, lugar, fundo) com suas camadas de baixo para cima, e qual é a ativa. "+
			"Use antes de editar e sempre que precisar de ids.",
		studioReadArgs{})
}

func (t *studioReadTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioReadArgs
	if err := decodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	detail := a.Detail
	if detail == "" {
		detail = "summary"
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenRead, map[string]string{"detail": detail})
	if err != nil {
		return studioFailure(err.Error())
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply)}
}

type studioLookArgs struct {
	TimesMS    []int64 `json:"times_ms,omitempty" desc:"vídeo: até 9 instantes em ms para ver nesses pontos exatos"`
	Count      int     `json:"count,omitempty" desc:"vídeo: quantos quadros (1 a 9) quando times_ms está vazio; padrão 6, um por trecho entre cortes"`
	MediaID    string  `json:"media_id,omitempty" id:"true" desc:"opcional: ver um vídeo ou imagem da biblioteca (anexo, gerado ou do projeto) em vez do projeto"`
	Marks      bool    `json:"marks,omitempty" desc:"opcional: numera cada camada ou clipe visível na imagem e liga cada número ao id na resposta, para apontar o que você vê"`
	ArtboardID string  `json:"artboard_id,omitempty" desc:"imagem: a prancheta a olhar (id de studio_read); all mostra todas lado a lado, numeradas; sem ele, a prancheta que o usuário está vendo"`
}

func (a studioLookArgs) issue() error {
	if len(a.TimesMS) > MaxStudioLookFrames || a.Count < 0 || a.Count > MaxStudioLookFrames {
		return fmt.Errorf("%w: até %d quadros por vez", errInvalidArgs, MaxStudioLookFrames)
	}
	for _, at := range a.TimesMS {
		if at < 0 || at > mediagen.MaxVideoMS {
			return fmt.Errorf("%w: times_ms fora do vídeo", errInvalidArgs)
		}
	}
	return nil
}

type studioLookTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioLookTool(deps StudioDeps) copilot.Tool {
	return &studioLookTool{deps: deps}
}

func (t *studioLookTool) Meta() copilot.Meta { return studioReadMeta() }

func (t *studioLookTool) Definition() tools.Definition {
	return definition("studio_look",
		"Mostra para você uma imagem do que está na tela do editor: no vídeo, uma folha com até 9 quadros rotulados com o tempo e os clipes visíveis "+
			"(do jeito que o vídeo será exportado, com textos e animações); na imagem, uma prancheta inteira, ou todas lado a lado com artboard_id all. Com media_id, mostra quadros de um vídeo ou "+
			"a imagem da biblioteca, para entender o material antes de cortar. Use para conferir o resultado antes de dizer que terminou.",
		studioLookArgs{})
}

func (t *studioLookTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioLookArgs
	if err := decodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if err := a.issue(); err != nil {
		return studioFailure(err.Error())
	}
	if !cc.SeesImages {
		return studioFailure(studioBlind)
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenLook, a)
	if err != nil {
		return studioFailure(err.Error())
	}
	if len(reply.Images) != 1 {
		return studioFailure(studioNoCapture)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply), Images: reply.Images}
}
