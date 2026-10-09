package copilottools

import (
	"context"
	"fmt"

	"vozko/domain/copilot"
	"vozko/domain/tools"
)

const (
	MinStudioExportQuality = 0.5
	MaxStudioExportScale   = 2
)

type studioExportArgs struct {
	Format      string   `json:"format,omitempty" enum:"png,jpeg" desc:"imagem: formato do arquivo (padrão png)"`
	Scale       int      `json:"scale,omitempty" desc:"imagem: 1 (padrão) ou 2 vezes o tamanho da prancheta"`
	Artboards   string   `json:"artboards,omitempty" enum:"active,all" desc:"imagem: active (padrão) exporta a prancheta aberta, all exporta todas, uma mídia por prancheta"`
	Quality     *float64 `json:"quality,omitempty" desc:"imagem em jpeg: qualidade de 0.5 a 1 (padrão 0.9)"`
	Transparent *bool    `json:"transparent,omitempty" desc:"imagem em png: true deixa transparente a prancheta sem fundo (padrão true)"`
}

func (a studioExportArgs) issue() error {
	if a.Scale < 0 || a.Scale > MaxStudioExportScale {
		return fmt.Errorf("%w: scale vai de 1 a %d", errInvalidArgs, MaxStudioExportScale)
	}
	if a.Quality != nil && (*a.Quality < MinStudioExportQuality || *a.Quality > 1) {
		return fmt.Errorf("%w: quality vai de %.1f a 1", errInvalidArgs, MinStudioExportQuality)
	}
	return nil
}

type studioExportTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioExportTool(deps StudioDeps) copilot.Tool {
	return &studioExportTool{deps: deps}
}

func (t *studioExportTool) Meta() copilot.Meta { return studioEditMeta() }

func (t *studioExportTool) Definition() tools.Definition {
	return definition("studio_export",
		"Exporta o projeto aberto para a biblioteca de mídia, como o botão Exportar do editor. No vídeo, começa a gerar o MP4 no navegador do usuário e responde na hora; "+
			"o andamento e o media_id final aparecem em studio_read, no campo export. Na imagem, gera PNG ou JPEG das pranchetas e devolve os media_id salvos. "+
			"Use quando o usuário pedir para exportar, salvar o resultado ou ter o arquivo pronto.",
		studioExportArgs{})
}

func (t *studioExportTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioExportArgs
	if err := decodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if err := a.issue(); err != nil {
		return studioFailure(err.Error())
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenExport, a)
	if err != nil {
		return studioFailure(err.Error())
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply)}
}
