package attendancetools

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/mathexpr"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

type calculateTool struct{}

type calculateArgs struct {
	Expressions []string `json:"expressions" req:"true" desc:"contas em ordem, uma por item; \"nome = conta\" guarda o resultado com esse nome para as contas seguintes. Ex.: [\"w = 640\", \"x = round(w / 2 + 180 * cos(rad(-35)), 2)\", \"pct(x, w)\"]"`
}

type calculatedStep struct {
	Name       string  `json:"name,omitempty"`
	Expression string  `json:"expression"`
	Result     float64 `json:"result"`
}

func NewCalculateTool() copilot.Tool { return calculateTool{} }

func (calculateTool) Meta() copilot.Meta { return copilottools.ChatReadMeta() }

func (calculateTool) OfferedOn(copilot.View) bool { return true }

func (calculateTool) Definition() tools.Definition {
	return copilottools.Definition("calculate",
		"Calcula com exatidão uma ou várias contas numa chamada. Use SEMPRE que for somar, dividir, tirar média, percentual, "+
			"variação, projeção, medidas, proporções, ângulos ou coordenadas; nunca faça contas de cabeça. Operadores + - * / % ^ e parênteses; "+
			"constantes pi e e; funções abs, sqrt, pow, exp, ln, log10, floor, ceil, round(x, casas), min, max, sum, avg, median, "+
			"pct(parte, todo), pct_change(de, para), lerp(de, para, t), clamp(x, mínimo, máximo), hypot(x, y), "+
			"sin, cos, tan, asin, acos, atan, atan2(y, x) em radianos, rad(graus) e deg(radianos). Só números e nomes criados nas contas anteriores.",
		calculateArgs{})
}

func (calculateTool) Execute(_ context.Context, _ copilot.Context, args map[string]interface{}) copilot.Result {
	steps, err := mathexpr.EvaluateSteps(copilottools.ArgStringList(args, "expressions"))
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	out := make([]calculatedStep, len(steps))
	for i, step := range steps {
		out[i] = calculatedStep{Name: step.Name, Expression: step.Expression, Result: step.Value}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"steps": out}}
}

type queryDatasetTool struct{}

func NewQueryDatasetTool() copilot.Tool { return queryDatasetTool{} }

func (queryDatasetTool) Meta() copilot.Meta { return copilottools.ChatReadMeta() }

func (queryDatasetTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "query_dataset",
		Description: "Lê linhas de um dataset devolvido por outra ferramenta nesta mesma resposta, ordenando por uma coluna e " +
			"paginando (até 25 linhas por chamada). Use em vez de pedir a lista inteira: as estatísticas (min, max, soma, " +
			"média) de cada coluna já vêm junto do dataset.",
		Parameters: map[string]tools.Parameter{
			"dataset_id": {Type: "string", Description: "id do dataset (ex.: ds1)"},
			"sort_by":    {Type: "string", Description: "coluna para ordenar"},
			"descending": {Type: "boolean", Description: "ordem decrescente"},
			"offset":     {Type: "integer", Description: "a partir de qual linha (padrão 0)"},
			"limit":      {Type: "integer", Description: "quantas linhas (1 a 25)"},
		},
		Required: []string{"dataset_id"},
	}
}

func (queryDatasetTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	d, err := cc.Datasets.Get(copilottools.ArgString(args, "dataset_id"))
	if err != nil {
		return datasetFailure(err)
	}
	descending := copilottools.ArgBoolPtr(args, "descending")
	page, err := d.Slice(copilot.SliceQuery{
		SortBy:     copilottools.ArgString(args, "sort_by"),
		Descending: descending != nil && *descending,
		Offset:     copilottools.ArgInt(args, "offset"),
		Limit:      copilottools.ArgInt(args, "limit"),
	})
	if err != nil {
		return datasetFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: page}
}

type renderChartTool struct{}

func NewRenderChartTool() copilot.Tool { return renderChartTool{} }

func (renderChartTool) Meta() copilot.Meta { return copilottools.ChatReadMeta() }

func (renderChartTool) Definition() tools.Definition {
	types := make([]string, 0, len(copilot.ChartTypes()))
	for _, t := range copilot.ChartTypes() {
		types = append(types, string(t))
	}
	return tools.Definition{
		Name: "render_chart",
		Description: "Mostra um gráfico ao usuário, logo abaixo da sua resposta. Prefira dataset_id (a tabela de outra ferramenta, " +
			"montada no servidor sem você copiar valores); use data só para números que você mesmo calculou. Tipos: " +
			strings.Join(types, ", ") + ". Barras até 60 categorias (use sort_by e limit para um top N), linhas e áreas até 400 " +
			"pontos, pizza e rosca agrupam o excedente em \"outros\", tabela até 50 linhas. Séries do mesmo gráfico precisam da " +
			"mesma unidade. Não repita no texto os valores que o gráfico já mostra; comente o que eles significam.",
		Parameters: map[string]tools.Parameter{
			"type":       {Type: "string", Description: "tipo do gráfico", Enum: types},
			"title":      {Type: "string", Description: "título curto, no idioma do usuário"},
			"subtitle":   {Type: "string", Description: "período e escopo, ex.: 01/09 a 25/09 · Suporte"},
			"dataset_id": {Type: "string", Description: "dataset de outra ferramenta"},
			"x":          {Type: "string", Description: "coluna do eixo X ou das fatias (ignorado quando você envia data)"},
			"y":          {Type: "array", Description: "colunas numéricas, uma por série (até 5)", Items: &tools.ParameterItems{Type: "string"}},
			"sort_by":    {Type: "string", Description: "coluna para ordenar antes de desenhar"},
			"descending": {Type: "boolean", Description: "ordem decrescente"},
			"limit":      {Type: "integer", Description: "mantém só as N primeiras linhas após ordenar"},
			"data":       {Type: "object", Description: "dados próprios em vez de dataset_id: {categories: [..], series: [{label, values: [..]}], unit: number|percent|minutes|money}. As séries viram as colunas s1, s2..."},
		},
		Required: []string{"type"},
	}
}

func (renderChartTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	d, err := chartSource(cc, args)
	if err != nil {
		return datasetFailure(err)
	}
	y := copilottools.ArgStringList(args, "y")
	x := copilottools.ArgString(args, "x")
	if _, inline := args["data"]; inline {
		x = copilot.InlineCategoryKey
		if len(y) == 0 {
			for _, c := range d.Columns[1:] {
				y = append(y, c.Key)
			}
		}
	}
	descending := copilottools.ArgBoolPtr(args, "descending")
	chart, err := copilot.BuildChart(d, copilot.ChartRequest{
		Type:       copilot.ChartType(copilottools.ArgString(args, "type")),
		Title:      copilottools.ArgString(args, "title"),
		Subtitle:   copilottools.ArgString(args, "subtitle"),
		X:          x,
		Y:          y,
		SortBy:     copilottools.ArgString(args, "sort_by"),
		Descending: descending != nil && *descending,
		Limit:      copilottools.ArgInt(args, "limit"),
	})
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	points := len(chart.Categories)
	if chart.Type == copilot.ChartScatter {
		points = len(chart.XValues)
	}
	return copilot.Result{
		Status: copilot.StatusOK,
		Chart:  &chart,
		Data:   map[string]interface{}{"rendered": chart.Type, "series": len(chart.Series), "points": points},
	}
}

func chartSource(cc copilot.Context, args map[string]interface{}) (*copilot.Dataset, error) {
	raw, inline := args["data"].(map[string]interface{})
	if !inline {
		return cc.Datasets.Get(copilottools.ArgString(args, "dataset_id"))
	}
	var series []copilot.InlineSeries
	for _, item := range asList(raw["series"]) {
		m, _ := item.(map[string]interface{})
		label, _ := m["label"].(string)
		values, err := numberList(m["values"])
		if err != nil {
			return nil, err
		}
		series = append(series, copilot.InlineSeries{Label: strings.TrimSpace(label), Values: values})
	}
	unit, _ := raw["unit"].(string)
	return copilot.InlineDataset(copilottools.ArgString(args, "title"), copilottools.ArgStringList(raw, "categories"), series, copilot.ColumnKind(unit))
}

func asList(v interface{}) []interface{} {
	list, _ := v.([]interface{})
	return list
}

func numberList(v interface{}) ([]float64, error) {
	list := asList(v)
	out := make([]float64, 0, len(list))
	for _, item := range list {
		n, ok := item.(float64)
		if !ok {
			return nil, errors.New("copilot: series values must be numbers")
		}
		out = append(out, n)
	}
	return out, nil
}

func datasetFailure(err error) copilot.Result {
	return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
}
