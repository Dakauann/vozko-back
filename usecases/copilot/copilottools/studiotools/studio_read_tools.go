package studiotools

import (
	"context"
	"fmt"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/screencall"
)

const (
	MaxStudioLookFrames = 9
	studioBlind         = "o modelo desta conversa não enxerga imagens, então nada foi visto; diga isso ao usuário sem descrever o quadro e sugira trocar, no seletor de modelos, para um modelo com visão"
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
	return copilottools.Definition("studio_read",
		"Lê o projeto aberto no Estúdio como ele está agora na tela, inclusive o que o usuário mudou desde a sua última leitura: "+
			"formato, duração, faixas de baixo para cima com seus clipes (id, tipo, mídia, início, duração, recorte, textos, animações), "+
			"marcadores, posição do cursor de tempo, seleção e trabalhos na fila. Em projetos de imagem: cada prancheta (id, nome, tamanho, lugar, fundo) com suas camadas de baixo para cima, e qual é a ativa. "+
			"alerts traz problemas medidos no documento (sombras, alinhamento, camadas, movimento). Use antes de editar e sempre que precisar de ids.",
		studioReadArgs{})
}

func (t *studioReadTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioReadArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
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
		return screencall.Result(cc, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screencall.Data(reply)}
}

type studioLookRegion struct {
	X float64 `json:"x" desc:"centro horizontal, de 0 a 1"`
	Y float64 `json:"y" desc:"centro vertical, de 0 a 1"`
	W float64 `json:"w" desc:"largura, de 0 a 1"`
	H float64 `json:"h" desc:"altura, de 0 a 1"`
}

func (r studioLookRegion) valid() bool {
	within := func(v float64) bool { return v >= 0 && v <= 1 }
	return within(r.X) && within(r.Y) && within(r.W) && within(r.H) && r.W > 0 && r.H > 0
}

type studioLookArgs struct {
	TimesMS    []int64           `json:"times_ms,omitempty" desc:"vídeo: até 9 instantes em ms para ver nesses pontos exatos"`
	Count      int               `json:"count,omitempty" desc:"vídeo: quantos quadros (1 a 9); padrão 6, um por trecho entre cortes, ou espalhados pela janela de focus_id ou from_ms e to_ms"`
	FocusID    string            `json:"focus_id,omitempty" desc:"vídeo: id de uma transição ou de um clipe; os quadros cobrem de perto o movimento dele (transição, entrada, saída, keyframes, animação do texto), do primeiro ao último quadro. imagem: id de uma camada; aproxima a camada com uma margem em volta, na prancheta dela"`
	View       string            `json:"view,omitempty" enum:"render,wireframe,guides,depth" desc:"render (padrão): a arte como sai na exportação; wireframe: o contorno real de cada camada, numerado e colorido pelo tipo (escondido, vazio, empilhado); guides: centro, terços, margem de 6%, linhas onde bordas e centros coincidem, eixo das inclinadas e o relatório layout (quase alinhados, margens, quase encostados, sobreposições, níveis de texto); depth: a direção de cada sombra, bordas que cortam sombras e camadas cobertas, com o relatório depth. No vídeo os relatórios vêm por quadro"`
	FromMS     *int64            `json:"from_ms,omitempty" desc:"vídeo: início de uma janela para ver em quadros próximos; vai junto com to_ms"`
	ToMS       *int64            `json:"to_ms,omitempty" desc:"vídeo: fim da janela começada em from_ms"`
	Region     *studioLookRegion `json:"region,omitempty" desc:"opcional: aproxima um trecho do quadro ou da prancheta para ler detalhes (legendas, texto pequeno, bordas); x e y são o centro, como nas transformações"`
	MediaID    string            `json:"media_id,omitempty" id:"true" desc:"opcional: ver um vídeo ou imagem da biblioteca (anexo, gerado ou do projeto) em vez do projeto"`
	Marks      bool              `json:"marks,omitempty" desc:"opcional: numera e contorna cada camada ou clipe visível na imagem e liga cada número ao id na resposta, para apontar o que você vê"`
	ArtboardID string            `json:"artboard_id,omitempty" desc:"imagem: a prancheta a olhar (id de studio_read); all mostra todas lado a lado, numeradas; sem ele, a prancheta que o usuário está vendo"`
}

func (a studioLookArgs) issue() error {
	if len(a.TimesMS) > MaxStudioLookFrames || a.Count < 0 || a.Count > MaxStudioLookFrames {
		return fmt.Errorf("%w: até %d quadros por vez", copilottools.ErrInvalidArgs, MaxStudioLookFrames)
	}
	for _, at := range a.TimesMS {
		if at < 0 || at > studio.MaxVideoMS {
			return fmt.Errorf("%w: times_ms fora do vídeo", copilottools.ErrInvalidArgs)
		}
	}
	if (a.FromMS == nil) != (a.ToMS == nil) {
		return fmt.Errorf("%w: from_ms e to_ms vão juntos", copilottools.ErrInvalidArgs)
	}
	if a.FromMS != nil && (*a.FromMS < 0 || *a.ToMS <= *a.FromMS || *a.ToMS > studio.MaxVideoMS) {
		return fmt.Errorf("%w: to_ms precisa ser maior que from_ms, dentro do vídeo", copilottools.ErrInvalidArgs)
	}
	modes := 0
	for _, used := range []bool{len(a.TimesMS) > 0, a.FocusID != "", a.FromMS != nil} {
		if used {
			modes++
		}
	}
	if modes > 1 {
		return fmt.Errorf("%w: use times_ms, focus_id ou from_ms com to_ms, um de cada vez", copilottools.ErrInvalidArgs)
	}
	if a.MediaID != "" && (modes > 0 || a.Region != nil || (a.View != "" && a.View != "render")) {
		return fmt.Errorf("%w: com media_id use só count; focus_id, janelas, region e view valem para o projeto", copilottools.ErrInvalidArgs)
	}
	if a.Region != nil && !a.Region.valid() {
		return fmt.Errorf("%w: region usa x, y, w e h de 0 a 1, com w e h maiores que 0", copilottools.ErrInvalidArgs)
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
	return copilottools.Definition("studio_look",
		"Mostra para você uma imagem do que está na tela do editor: no vídeo, uma folha com até 9 quadros rotulados com o tempo e os clipes visíveis "+
			"(do jeito que o vídeo será exportado, com transições, textos e animações); na imagem, uma prancheta inteira, ou todas lado a lado com artboard_id all. "+
			"A resposta traz, por quadro, os clipes visíveis e, para os que se movem, posição, tamanho, rotação, opacidade, desfoque e o progresso da transição ou da animação do texto naquele instante. "+
			"Para conferir movimento, use focus_id (uma transição ou um clipe) ou from_ms e to_ms: os quadros ficam próximos dentro da janela. "+
			"Quanto mais quadros, menor cada um: para ler legendas e texto pequeno, olhe 1 ou 2 quadros ou aproxime com region. "+
			"Na imagem, a resposta traz legibility (contraste medido nos pixels atrás de cada texto). alerts traz problemas medidos no documento (sombras, alinhamento, camadas; no vídeo, saltos, flashes, saídas da tela); no vídeo, focus_id ou from_ms trazem motion_report. Com media_id, mostra quadros de um vídeo ou "+
			"a imagem da biblioteca, para entender o material antes de cortar. Use para conferir o resultado antes de dizer que terminou.",
		studioLookArgs{})
}

func (t *studioLookTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioLookArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if err := a.issue(); err != nil {
		return studioFailure(err.Error())
	}
	if !cc.SeesImages {
		return studioFailure(studioBlind)
	}
	project, err := t.deps.openProject(ctx, cc)
	if err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenLook, a)
	if err != nil {
		return screencall.Result(cc, err)
	}
	if len(reply.Images) != 1 {
		return studioFailure(studioNoCapture)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screencall.Data(reply), Images: reply.Images, Subject: lookSubject(project.ID, reply.Target)}
}

func lookSubject(projectID string, target *copilot.ScreenTarget) *copilot.Subject {
	if target == nil {
		return nil
	}
	return &copilot.Subject{Kind: copilot.SubjectStudioLook, Key: target.Kind, Label: target.Name, Target: target.In(projectID)}
}
