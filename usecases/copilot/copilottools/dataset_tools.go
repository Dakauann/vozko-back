package copilottools

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/mathexpr"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

func chatReadMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

type calculateTool struct{}

func NewCalculateTool() copilot.Tool { return calculateTool{} }

func (calculateTool) Meta() copilot.Meta { return chatReadMeta() }

func (calculateTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "calculate",
		Description: "Calcula uma expressão numérica com exatidão. Use SEMPRE que for somar, dividir, tirar média, percentual, " +
			"variação ou projeção; nunca faça contas de cabeça. Operadores + - * / % ^ e parênteses; funções abs, sqrt, " +
			"round(x, casas), min, max, sum, avg, median, pct(parte, todo) e pct_change(de, para). Só números, sem nomes de métricas.",
		Parameters: map[string]tools.Parameter{
			"expression": {Type: "string", Description: "ex.: round(pct_change(1702, 1895), 1)"},
		},
		Required: []string{"expression"},
	}
}

func (calculateTool) Execute(_ context.Context, _ copilot.Context, args map[string]interface{}) copilot.Result {
	expr := argString(args, "expression")
	v, err := mathexpr.Evaluate(expr)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"expression": expr, "result": v}}
}

type queryDatasetTool struct{}

func NewQueryDatasetTool() copilot.Tool { return queryDatasetTool{} }

func (queryDatasetTool) Meta() copilot.Meta { return chatReadMeta() }

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
	d, err := cc.Datasets.Get(argString(args, "dataset_id"))
	if err != nil {
		return datasetFailure(err)
	}
	descending := argBoolPtr(args, "descending")
	page, err := d.Slice(copilot.SliceQuery{
		SortBy:     argString(args, "sort_by"),
		Descending: descending != nil && *descending,
		Offset:     argInt(args, "offset"),
		Limit:      argInt(args, "limit"),
	})
	if err != nil {
		return datasetFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: page}
}

type renderChartTool struct{}

func NewRenderChartTool() copilot.Tool { return renderChartTool{} }

func (renderChartTool) Meta() copilot.Meta { return chatReadMeta() }

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
	y := argStringList(args, "y")
	x := argString(args, "x")
	if _, inline := args["data"]; inline {
		x = copilot.InlineCategoryKey
		if len(y) == 0 {
			for _, c := range d.Columns[1:] {
				y = append(y, c.Key)
			}
		}
	}
	descending := argBoolPtr(args, "descending")
	chart, err := copilot.BuildChart(d, copilot.ChartRequest{
		Type:       copilot.ChartType(argString(args, "type")),
		Title:      argString(args, "title"),
		Subtitle:   argString(args, "subtitle"),
		X:          x,
		Y:          y,
		SortBy:     argString(args, "sort_by"),
		Descending: descending != nil && *descending,
		Limit:      argInt(args, "limit"),
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
		return cc.Datasets.Get(argString(args, "dataset_id"))
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
	return copilot.InlineDataset(argString(args, "title"), argStringList(raw, "categories"), series, copilot.ColumnKind(unit))
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
