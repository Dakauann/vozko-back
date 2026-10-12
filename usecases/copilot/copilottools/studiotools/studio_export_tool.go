package studiotools

import (
	"context"
	"fmt"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

const (
	MinStudioExportQuality   = 0.5
	MaxStudioExportScale     = 2
	MinStudioAnimationWidth  = 120
	MaxStudioAnimationWidth  = 1920
	MinStudioAnimationFPS    = 5
	MaxStudioAnimationFPS    = 30
	MinStudioGifColors       = 2
	MaxStudioGifColors       = 256
	MaxStudioAnimationLoop   = 65535
	studioExportDefaultImage = "png"
	studioExportDefaultVideo = "mp4"
)

var (
	studioImageExports = map[string]bool{"png": true, "jpeg": true, "pdf": true, "svg": true}
	studioVideoExports = map[string]bool{"mp4": true, "gif": true, "webp": true, "png": true, "jpeg": true}
	studioFilmExports  = map[string]bool{"mp4": true, "gif": true, "webp": true}
	studioExportsOf    = map[studio.Kind]map[string]bool{studio.KindImage: studioImageExports, studio.KindVideo: studioVideoExports}
)

type studioExportArgs struct {
	Format      string   `json:"format,omitempty" enum:"png,jpeg,pdf,svg,mp4,gif,webp" desc:"formato do arquivo. Imagem: png (padrão), jpeg, pdf (vetorial, todas as pranchetas num só arquivo) ou svg (vetorial, um arquivo por prancheta). Vídeo: mp4 (padrão), gif ou webp (animação sem som), ou png e jpeg para salvar um quadro como imagem (pede at_ms)"`
	Scale       int      `json:"scale,omitempty" desc:"imagem em png ou jpeg: 1 (padrão) ou 2 vezes o tamanho da prancheta"`
	Artboards   string   `json:"artboards,omitempty" enum:"active,all" desc:"imagem: active (padrão) exporta a prancheta aberta, all exporta todas"`
	Quality     *float64 `json:"quality,omitempty" desc:"jpeg ou webp: qualidade de 0.5 a 1 (padrão 0.9 no jpeg, 0.8 no webp)"`
	Transparent *bool    `json:"transparent,omitempty" desc:"imagem em png, pdf ou svg: true deixa transparente a prancheta sem fundo (padrão true)"`
	Preset      string   `json:"preset,omitempty" enum:"digital,print" desc:"pdf: digital (padrão, para tela) ou print (impressão, imagens internas a 300 ppi)"`
	Width       int      `json:"width,omitempty" desc:"gif ou webp: largura em px, de 120 até a largura do vídeo (padrão 480); a altura segue a proporção"`
	FPS         int      `json:"fps,omitempty" desc:"gif ou webp: quadros por segundo, de 5 a 30 (padrão 15)"`
	Colors      int      `json:"colors,omitempty" desc:"gif: cores da paleta, de 2 a 256 (padrão 256)"`
	Dither      string   `json:"dither,omitempty" enum:"none,diffusion,ordered" desc:"gif: none para artes chapadas, diffusion (padrão) para fotos e degradês, ordered para granulado estável entre quadros"`
	Loop        *int     `json:"loop,omitempty" desc:"gif ou webp: 0 repete sem parar (padrão), 1 toca uma vez, N toca N vezes"`
	FromMs      *int     `json:"from_ms,omitempty" desc:"vídeo em mp4, gif ou webp: começo do trecho exportado, em ms (padrão 0). Para exportar a área de trabalho marcada pelo usuário, use from_ms e to_ms de work_area em studio_read"`
	ToMs        *int     `json:"to_ms,omitempty" desc:"vídeo em mp4, gif ou webp: fim do trecho exportado, em ms, depois de from_ms e até a duração (padrão o fim do vídeo)"`
	AtMs        *int     `json:"at_ms,omitempty" desc:"vídeo em png ou jpeg: o instante do quadro salvo como imagem, em ms, de 0 até antes do fim; serve para capa de Reels ou imagem de anúncio"`
}

func (a studioExportArgs) format(kind studio.Kind) string {
	if a.Format != "" {
		return a.Format
	}
	if kind == studio.KindVideo {
		return studioExportDefaultVideo
	}
	return studioExportDefaultImage
}

func (a studioExportArgs) issue(kind studio.Kind) error {
	format := a.format(kind)
	if !studioExportsOf[kind][format] {
		return fmt.Errorf("%w: este projeto não exporta %s", copilottools.ErrInvalidArgs, format)
	}
	if err := a.optionsFit(kind, format); err != nil {
		return err
	}
	if kind == studio.KindVideo && !studioFilmExports[format] && a.AtMs == nil {
		return fmt.Errorf("%w: %s salva um quadro do vídeo; diga qual com at_ms", copilottools.ErrInvalidArgs, format)
	}
	return a.rangeIssue()
}

func (a studioExportArgs) optionsFit(kind studio.Kind, format string) error {
	image := kind == studio.KindImage
	raster := format == "png" || format == "jpeg"
	animation := format == "gif" || format == "webp"
	film := kind == studio.KindVideo && studioFilmExports[format]
	misplaced := map[string]bool{
		"scale":       a.Scale != 0 && !(image && raster),
		"artboards":   a.Artboards != "" && !image,
		"transparent": a.Transparent != nil && !image,
		"quality":     a.Quality != nil && format != "jpeg" && format != "webp",
		"preset":      a.Preset != "" && format != "pdf",
		"width":       a.Width != 0 && !animation,
		"fps":         a.FPS != 0 && !animation,
		"loop":        a.Loop != nil && !animation,
		"colors":      a.Colors != 0 && format != "gif",
		"dither":      a.Dither != "" && format != "gif",
		"from_ms":     a.FromMs != nil && !film,
		"to_ms":       a.ToMs != nil && !film,
		"at_ms":       a.AtMs != nil && (image || film),
	}
	for _, name := range []string{"scale", "artboards", "transparent", "quality", "preset", "width", "fps", "loop", "colors", "dither", "from_ms", "to_ms", "at_ms"} {
		if misplaced[name] {
			return fmt.Errorf("%w: %s não vale para %s", copilottools.ErrInvalidArgs, name, format)
		}
	}
	return nil
}

func (a studioExportArgs) rangeIssue() error {
	switch {
	case a.Scale < 0 || a.Scale > MaxStudioExportScale:
		return fmt.Errorf("%w: scale vai de 1 a %d", copilottools.ErrInvalidArgs, MaxStudioExportScale)
	case a.Quality != nil && (*a.Quality < MinStudioExportQuality || *a.Quality > 1):
		return fmt.Errorf("%w: quality vai de %.1f a 1", copilottools.ErrInvalidArgs, MinStudioExportQuality)
	case a.Width != 0 && (a.Width < MinStudioAnimationWidth || a.Width > MaxStudioAnimationWidth):
		return fmt.Errorf("%w: width vai de %d a %d", copilottools.ErrInvalidArgs, MinStudioAnimationWidth, MaxStudioAnimationWidth)
	case a.FPS != 0 && (a.FPS < MinStudioAnimationFPS || a.FPS > MaxStudioAnimationFPS):
		return fmt.Errorf("%w: fps vai de %d a %d", copilottools.ErrInvalidArgs, MinStudioAnimationFPS, MaxStudioAnimationFPS)
	case a.Colors != 0 && (a.Colors < MinStudioGifColors || a.Colors > MaxStudioGifColors):
		return fmt.Errorf("%w: colors vai de %d a %d", copilottools.ErrInvalidArgs, MinStudioGifColors, MaxStudioGifColors)
	case a.Loop != nil && (*a.Loop < 0 || *a.Loop > MaxStudioAnimationLoop):
		return fmt.Errorf("%w: loop vai de 0 a %d", copilottools.ErrInvalidArgs, MaxStudioAnimationLoop)
	case a.FromMs != nil && *a.FromMs < 0:
		return fmt.Errorf("%w: from_ms começa em 0", copilottools.ErrInvalidArgs)
	case a.ToMs != nil && *a.ToMs <= 0:
		return fmt.Errorf("%w: to_ms vem depois do começo do vídeo", copilottools.ErrInvalidArgs)
	case a.FromMs != nil && a.ToMs != nil && *a.ToMs <= *a.FromMs:
		return fmt.Errorf("%w: to_ms vem depois de from_ms", copilottools.ErrInvalidArgs)
	case a.AtMs != nil && *a.AtMs < 0:
		return fmt.Errorf("%w: at_ms começa em 0", copilottools.ErrInvalidArgs)
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
	return copilottools.Definition("studio_export",
		"Exporta o projeto aberto para a biblioteca de mídia, como o botão Exportar do editor. No vídeo, começa a gerar MP4, GIF ou WebP no navegador do usuário e responde na hora; "+
			"from_ms e to_ms exportam só um trecho (a área de trabalho marcada aparece em studio_read como work_area), e png ou jpeg com at_ms salvam um quadro como imagem; "+
			"o andamento e o media_id final aparecem em studio_read, no campo export. Na imagem, gera PNG, JPEG, PDF ou SVG das pranchetas e devolve os media_id salvos. "+
			"Use quando o usuário pedir para exportar, salvar o resultado ou ter o arquivo pronto.",
		studioExportArgs{})
}

func (t *studioExportTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a studioExportArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	project, err := t.deps.openProject(ctx, cc)
	if err != nil {
		return studioFailure(err.Error())
	}
	if err := a.issue(project.Kind); err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenExport, a)
	if err != nil {
		return screenResult(cc, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply)}
}
