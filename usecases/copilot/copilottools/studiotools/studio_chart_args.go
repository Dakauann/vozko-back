package studiotools

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"vozko/domain/studio"
)

type studioChartSeriesArgs struct {
	Name   string     `json:"name,omitempty" desc:"nome da série, mostrado na legenda"`
	Values []*float64 `json:"values" req:"true" desc:"um número por categoria, na mesma ordem de chart_labels; null deixa a célula vazia"`
}

type studioChartStyleArgs struct {
	Palette     []string `json:"palette,omitempty" desc:"cores das séries (ou das fatias na pizza e na rosca), em ordem, #rrggbb ou #rrggbbaa; repetem quando acabam; sem palette valem as cores dos estilos do projeto, quando há"`
	FontID      string   `json:"font_id,omitempty" desc:"fonte dos textos do gráfico"`
	FontSize    *float64 `json:"font_size,omitempty" desc:"altura do texto como fração da altura do gráfico, de 0.02 a 0.2 (0.05 padrão)"`
	TextColor   string   `json:"text_color,omitempty" desc:"cor dos rótulos, eixos e legenda"`
	GridColor   string   `json:"grid_color,omitempty" desc:"cor das linhas de grade, #rrggbb ou #rrggbbaa (o alfa no fim deixa translúcida)"`
	Axes        *bool    `json:"axes,omitempty" desc:"mostra os eixos com os rótulos das categorias e dos valores"`
	Gridlines   *bool    `json:"gridlines,omitempty" desc:"mostra as linhas de grade"`
	Legend      string   `json:"legend,omitempty" enum:"none,top,bottom,right" desc:"onde fica a legenda"`
	ValueLabels *bool    `json:"value_labels,omitempty" desc:"escreve o valor em cada barra, ponto ou fatia"`
	BarRadius   *float64 `json:"bar_radius,omitempty" desc:"arredondamento dos cantos das barras, de 0 a 1"`
	BarGap      *float64 `json:"bar_gap,omitempty" desc:"espaço livre entre as barras, de 0 a 0.9 da faixa de cada categoria"`
	LineWidth   *float64 `json:"line_width,omitempty" desc:"espessura das linhas como fração da altura do gráfico, de 0.001 a 0.05"`
	Smooth      *bool    `json:"smooth,omitempty" desc:"linhas curvas suaves em vez de retas entre os pontos"`
	Points      *bool    `json:"points,omitempty" desc:"marca cada ponto das linhas"`
	Hole        *float64 `json:"hole,omitempty" desc:"furo da rosca, de 0.1 a 0.9 do raio"`
}

type studioChartFormatArgs struct {
	Locale   string  `json:"locale,omitempty" enum:"pt-BR,en-US,es-ES,de-DE" desc:"separadores dos números: pt-BR, es-ES e de-DE escrevem 1.234,5 e en-US escreve 1,234.5"`
	Decimals *int    `json:"decimals,omitempty" desc:"casas decimais, de 0 a 3"`
	Prefix   *string `json:"prefix,omitempty" desc:"texto antes dos números, até 16 caracteres, como R$; vazio remove"`
	Suffix   *string `json:"suffix,omitempty" desc:"texto depois dos números, até 16 caracteres, como % ou mil; vazio remove"`
	Percent  *bool   `json:"percent,omitempty" desc:"pizza e rosca: os rótulos mostram a porcentagem de cada fatia em vez do valor"`
}

type studioChartArgs struct {
	ChartKind   string                  `json:"chart_kind,omitempty" enum:"bar,barHorizontal,barGrouped,barStacked,line,lineMulti,area,areaStacked,pie,donut,scatter" desc:"add_chart e update_chart_data: o tipo do gráfico; bar, barHorizontal, line, area, pie e donut usam a primeira série; barGrouped, barStacked, lineMulti e areaStacked usam todas; scatter usa a primeira série como eixo x e as outras como pontos"`
	ChartLabels []string                `json:"chart_labels,omitempty" desc:"add_chart e update_chart_data: o nome de cada categoria (eixo x, linhas ou fatias), na ordem"`
	ChartSeries []studioChartSeriesArgs `json:"chart_series,omitempty" desc:"add_chart e update_chart_data: as séries de números, cada uma com um valor por categoria; update_chart_data troca todas as séries de uma vez"`
	ChartStyle  *studioChartStyleArgs   `json:"chart_style,omitempty" desc:"add_chart e update_chart_data: a aparência; os campos omitidos ficam como estão"`
	ChartFormat *studioChartFormatArgs  `json:"chart_format,omitempty" desc:"add_chart e update_chart_data: o formato dos números; os campos omitidos ficam como estão"`
}

type studioChartMotionArgs struct {
	ChartEntrance string `json:"chart_entrance,omitempty" enum:"none,grow,draw,sweep,fade" desc:"animate_chart: grow sobe as barras a partir da base, draw traça as linhas, sweep abre a pizza e a rosca girando, fade faz cada parte aparecer; none tira a animação"`
	ChartOrder    string `json:"chart_order,omitempty" enum:"series,category" desc:"animate_chart: a vez de cada parte, por série ou por categoria"`
}

func chartInRange(v *float64, low, high float64) bool { return v == nil || (*v >= low && *v <= high) }

func (a studioChartArgs) argsIssue() string {
	for i, series := range a.ChartSeries {
		if a.ChartLabels != nil && len(series.Values) != len(a.ChartLabels) {
			return fmt.Sprintf("chart_series[%d] tem %d valores e chart_labels tem %d categorias; mande um valor por categoria", i, len(series.Values), len(a.ChartLabels))
		}
		for _, value := range series.Values {
			if !chartInRange(value, studio.MinCounterValue, studio.MaxCounterValue) {
				return fmt.Sprintf("chart_series[%d]: os valores vão de %g a %g", i, studio.MinCounterValue, studio.MaxCounterValue)
			}
		}
		if utf8.RuneCountInString(series.Name) > studio.MaxChartNameRunes {
			return fmt.Sprintf("chart_series[%d].name passa de %d caracteres", i, studio.MaxChartNameRunes)
		}
	}
	for i, label := range a.ChartLabels {
		if utf8.RuneCountInString(label) > studio.MaxChartLabelRunes || strings.ContainsAny(label, "\n\r\t") {
			return fmt.Sprintf("chart_labels[%d] precisa ser uma linha de até %d caracteres", i, studio.MaxChartLabelRunes)
		}
	}
	if msg := a.ChartStyle.issue(); msg != "" {
		return msg
	}
	return a.ChartFormat.issue()
}

func (s *studioChartStyleArgs) issue() string {
	if s == nil {
		return ""
	}
	for _, color := range append(slices.Clone(s.Palette), s.TextColor, s.GridColor) {
		if color != "" && !chartColor(color) {
			return "as cores do gráfico são #rrggbb ou #rrggbbaa"
		}
	}
	switch {
	case s.FontID != "" && !slices.Contains(studio.Fonts(), s.FontID):
		return fmt.Sprintf("chart_style.font_id %q não existe", s.FontID)
	case !chartInRange(s.FontSize, studio.MinChartFontSize, studio.MaxChartFontSize):
		return fmt.Sprintf("chart_style.font_size vai de %g a %g", studio.MinChartFontSize, studio.MaxChartFontSize)
	case !chartInRange(s.LineWidth, studio.MinChartLineWidth, studio.MaxChartLineWidth):
		return fmt.Sprintf("chart_style.line_width vai de %g a %g", studio.MinChartLineWidth, studio.MaxChartLineWidth)
	case !chartInRange(s.BarRadius, 0, 1) || !chartInRange(s.BarGap, 0, studio.MaxChartBarGap):
		return fmt.Sprintf("chart_style.bar_radius vai de 0 a 1 e chart_style.bar_gap de 0 a %g", studio.MaxChartBarGap)
	case !chartInRange(s.Hole, studio.MinChartHole, studio.MaxChartHole):
		return fmt.Sprintf("chart_style.hole vai de %g a %g", studio.MinChartHole, studio.MaxChartHole)
	case s.Legend != "" && !slices.Contains(studio.ChartLegends(), s.Legend):
		return "chart_style.legend deve ser um de: " + strings.Join(studio.ChartLegends(), ", ")
	}
	return ""
}

func (f *studioChartFormatArgs) issue() string {
	if f == nil {
		return ""
	}
	switch {
	case f.Locale != "" && !slices.Contains(studio.CounterLocales(), f.Locale):
		return "chart_format.locale deve ser um de: " + strings.Join(studio.CounterLocales(), ", ")
	case f.Decimals != nil && (*f.Decimals < 0 || *f.Decimals > studio.MaxCounterDecimals):
		return fmt.Sprintf("chart_format.decimals vai de 0 a %d", studio.MaxCounterDecimals)
	case f.Prefix != nil && utf8.RuneCountInString(*f.Prefix) > studio.MaxCounterAffixRunes, f.Suffix != nil && utf8.RuneCountInString(*f.Suffix) > studio.MaxCounterAffixRunes:
		return fmt.Sprintf("chart_format.prefix e chart_format.suffix têm até %d caracteres", studio.MaxCounterAffixRunes)
	}
	return ""
}

func chartColor(value string) bool {
	if len(value) != 7 && len(value) != 9 || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func (o studioVideoOperation) chartIssue() string {
	if msg := o.studioChartArgs.argsIssue(); msg != "" {
		return msg
	}
	if o.Op != "animate_chart" {
		return ""
	}
	if o.DurationMS != nil && (*o.DurationMS < studio.MinChartAnimationMS || *o.DurationMS > studio.MaxChartAnimationMS) {
		return fmt.Sprintf("animate_chart: duration_ms vai de %d a %d", studio.MinChartAnimationMS, studio.MaxChartAnimationMS)
	}
	if o.StaggerMS != nil && (*o.StaggerMS < 0 || *o.StaggerMS > studio.MaxChartStaggerMS) {
		return fmt.Sprintf("animate_chart: stagger_ms vai de 0 a %d", studio.MaxChartStaggerMS)
	}
	return ""
}

const studioChartIntro = "Gráficos de verdade, em vez de barras desenhadas à mão: o gráfico guarda os dados e se desenha sozinho com formas e textos comuns (eixos, grade, rótulos, legenda e números no formato do idioma), igual na tela e na exportação. " +
	"Os dados são categorias (chart_labels) e séries (chart_series, cada uma com name e values, um número por categoria). " +
	"update_chart_data troca só o que vier (chart_labels, chart_series, chart_kind, chart_style, chart_format) e mantém o resto; studio_read mostra o gráfico em chart, com os mesmos campos. " +
	"Exemplo: {\"op\":\"add_chart\",\"chart_kind\":\"barGrouped\",\"chart_labels\":[\"Jan\",\"Fev\",\"Mar\"],\"chart_series\":[{\"name\":\"2025\",\"values\":[120,180,150]},{\"name\":\"2026\",\"values\":[140,210,190]}],\"chart_format\":{\"prefix\":\"R$ \"}}. "

const studioChartImageGuide = studioChartIntro +
	"add_chart(chart_kind, chart_labels, chart_series, chart_style?, chart_format?, name?, artboard_id?, x?, y?, w?, h?), update_chart_data(layer_id, os campos que mudam), expand_chart(layer_id: transforma o gráfico em formas e textos comuns num grupo, para retoques; os dados deixam de ser editáveis). "

const studioChartVideoGuide = studioChartIntro +
	"add_chart(chart_kind, chart_labels, chart_series, chart_style?, chart_format?, at_ms, duration_ms?, track_id?, x?, y?, w?, h?), update_chart_data(clip_id, os campos que mudam), " +
	"animate_chart(clip_id, chart_entrance, chart_order?, duration_ms? de cada parte de 100 a 10000, stagger_ms? entre uma parte e a seguinte de 0 a 5000, easing?): a entrada anima os dados enquanto eixos e legenda ficam parados. "
