package emaildesigntools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/screencall"
)

type emailReadArgs struct{}

type emailReadTool struct {
	designScope
	deps Deps
}

func NewEmailReadTool(deps Deps) copilot.Tool {
	return &emailReadTool{deps: deps}
}

func (t *emailReadTool) Meta() copilot.Meta { return readMeta() }

func (t *emailReadTool) Definition() tools.Definition {
	return copilottools.Definition("email_read",
		"Lê o design de e-mail aberto como ele está agora na tela, inclusive o que o usuário mudou desde a sua última leitura: "+
			"nome, bloco selecionado, settings (largura, cores, fonte, preheader) e os blocos de cima para baixo com id, kind e os mesmos campos que email_edit aceita "+
			"(texto de bloco de texto em marcação curta; colunas trazem os blocos de cada coluna). "+
			"review traz hints e checks (contraste, hierarquia, ações, largura); open_gaps, no fim, lista o que impede salvar e enviar. Use antes de editar e sempre que precisar de ids.",
		emailReadArgs{})
}

func (t *emailReadTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	if _, err := t.deps.openDesign(ctx, cc); err != nil {
		return failure(err.Error())
	}
	live, err := readLive(ctx, cc)
	if err != nil {
		return screencall.Result(cc, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: withOpenGaps(map[string]interface{}{
		"name":     live.Name,
		"selected": live.Selected,
		"settings": live.Document.Settings,
		"blocks":   blockView(live.Document.Blocks),
		"review":   emaildesign.Review(live.Document),
	}, live.Document)}
}
