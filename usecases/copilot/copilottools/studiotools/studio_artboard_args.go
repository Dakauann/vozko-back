package studiotools

import (
	"fmt"
	"math"

	"vozko/domain/studio"
)

const maxArrangeGap = 10_000

type studioArtboardArgs struct {
	ArtboardIDs    []string `json:"artboard_ids,omitempty" desc:"arrange_artboards: as pranchetas a organizar (ids de studio_read ou @ref), nesta ordem quando arrange_order é given; sem elas, todas"`
	ArtboardX      *float64 `json:"artboard_x,omitempty" desc:"update_artboard: nova posição horizontal da prancheta no projeto, em pixels, pelo canto de cima à esquerda, como o x da prancheta em studio_read; de -100000 a 100000"`
	ArtboardY      *float64 `json:"artboard_y,omitempty" desc:"update_artboard: nova posição vertical da prancheta no projeto, em pixels, pelo canto de cima à esquerda, como o y da prancheta em studio_read; de -100000 a 100000"`
	ArrangeLayout  string   `json:"arrange_layout,omitempty" enum:"row,column,grid" desc:"arrange_artboards, obrigatório: row lado a lado, column uma embaixo da outra, grid em linhas e colunas"`
	ArrangeGap     *float64 `json:"arrange_gap,omitempty" desc:"arrange_artboards: espaço igual entre as pranchetas em pixels, de 0 a 10000 (padrão 100); 0 encosta uma na outra"`
	ArrangeOrder   string   `json:"arrange_order,omitempty" enum:"position,name,given" desc:"arrange_artboards: position segue a ordem de leitura atual (padrão), name ordena pelo nome com os números em ordem natural, given segue a ordem de artboard_ids"`
	ArrangeAlign   string   `json:"arrange_align,omitempty" enum:"start,center,end" desc:"arrange_artboards, para tamanhos diferentes: na linha e na grade start alinha pelo topo e end pela base; na coluna start alinha pela esquerda e end pela direita (padrão start)"`
	ArrangeColumns *int     `json:"arrange_columns,omitempty" desc:"arrange_artboards com grid: colunas, a partir de 1 (padrão a raiz quadrada do total, arredondada para cima)"`
	ArrangeBySize  *bool    `json:"arrange_by_size,omitempty" desc:"arrange_artboards: true separa por tamanho, uma linha (ou coluna) para cada tamanho, na ordem em que os tamanhos aparecem"`
}

func (o studioImageOperation) artboardIssue() string {
	if o.Op == "arrange_artboards" && o.ArrangeLayout == "" {
		return "arrange_artboards precisa de arrange_layout: row, column ou grid"
	}
	for _, place := range []struct {
		name  string
		value *float64
	}{{"artboard_x", o.ArtboardX}, {"artboard_y", o.ArtboardY}} {
		if place.value != nil && math.Abs(*place.value) > studio.MaxArtboardCoordinate {
			return fmt.Sprintf("%s vai de -%d a %d pixels", place.name, studio.MaxArtboardCoordinate, studio.MaxArtboardCoordinate)
		}
	}
	if o.ArrangeGap != nil && (*o.ArrangeGap < 0 || *o.ArrangeGap > maxArrangeGap) {
		return fmt.Sprintf("arrange_gap vai de 0 a %d pixels", maxArrangeGap)
	}
	if o.ArrangeColumns != nil && *o.ArrangeColumns < 1 {
		return "arrange_columns precisa ser um número inteiro a partir de 1"
	}
	return ""
}
