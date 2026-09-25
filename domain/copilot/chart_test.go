package copilot

import (
	"errors"
	"fmt"
	"testing"
)

func monthlyDataset(t *testing.T, months int) *Dataset {
	t.Helper()
	d := NewDataset("Finalizadas por mês", []Column{
		{Key: "month", Label: "Mês", Kind: ColumnDate},
		{Key: "human", Label: "Humano", Kind: ColumnNumber},
		{Key: "ai", Label: "IA", Kind: ColumnNumber},
		{Key: "frt", Label: "FRT", Kind: ColumnMinutes},
	})
	for i := 0; i < months; i++ {
		if err := d.AddRow(fmt.Sprintf("2026-%02d", i%12+1), float64(10+i), float64(i), float64(3*i)); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestBuildChartFromADataset(t *testing.T) {
	c, err := BuildChart(monthlyDataset(t, 6), ChartRequest{Type: ChartStackedBar, Title: "Volume", X: "month", Y: []string{"human", "ai"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != ChartStackedBar || c.Title != "Volume" || len(c.Categories) != 6 || len(c.Series) != 2 {
		t.Fatalf("chart = %+v", c)
	}
	if c.Series[0].Label != "Humano" || c.Series[0].Kind != ColumnNumber || *c.Series[0].Values[5] != 15 {
		t.Fatalf("series = %+v", c.Series[0])
	}
	if c.XLabel != "Mês" || c.Categories[0] != "2026-01" {
		t.Fatalf("x = %q %v", c.XLabel, c.Categories)
	}
}

func TestBuildChartDefaultsTheTitleToTheDataset(t *testing.T) {
	c, err := BuildChart(monthlyDataset(t, 2), ChartRequest{Type: ChartLine, X: "month", Y: []string{"frt"}})
	if err != nil || c.Title != "Finalizadas por mês" {
		t.Fatalf("title = %q, err %v", c.Title, err)
	}
}

func TestBuildChartTopN(t *testing.T) {
	c, err := BuildChart(monthlyDataset(t, 12), ChartRequest{Type: ChartHorizontalBar, X: "month", Y: []string{"ai"}, SortBy: "ai", Descending: true, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Categories) != 3 || c.Categories[0] != "2026-12" || *c.Series[0].Values[0] != 11 {
		t.Fatalf("chart = %v %v", c.Categories, c.Series[0].Values)
	}
}

func TestPieFoldsTheTailIntoOther(t *testing.T) {
	c, err := BuildChart(monthlyDataset(t, 20), ChartRequest{Type: ChartPie, X: "month", Y: []string{"human"}})
	if err != nil {
		t.Fatal(err)
	}
	// Twenty slices is unreadable; the tail survives as one slice so the total stays honest.
	if len(c.Categories) != MaxPieSlices || c.Categories[MaxPieSlices-1] != OtherCategory {
		t.Fatalf("categories = %v", c.Categories)
	}
	total := 0.0
	for _, v := range c.Series[0].Values {
		total += *v
	}
	if total != 390 {
		t.Fatalf("total = %v, want every row accounted for (390)", total)
	}
}

func TestTableCarriesTheChosenColumns(t *testing.T) {
	c, err := BuildChart(monthlyDataset(t, 3), ChartRequest{Type: ChartTable, X: "month", Y: []string{"frt"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Categories) != 3 || len(c.Series) != 1 || c.Series[0].Key != "frt" {
		t.Fatalf("table = %+v", c)
	}
}

func TestScatterNeedsNumericAxes(t *testing.T) {
	c, err := BuildChart(monthlyDataset(t, 4), ChartRequest{Type: ChartScatter, X: "human", Y: []string{"frt"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.XValues) != 4 || *c.XValues[3] != 13 {
		t.Fatalf("x values = %v", c.XValues)
	}
	if _, err := BuildChart(monthlyDataset(t, 4), ChartRequest{Type: ChartScatter, X: "month", Y: []string{"frt"}}); !errors.Is(err, ErrInvalidChart) {
		t.Fatalf("err = %v, want a text x axis refused for scatter", err)
	}
}

func TestBuildChartRefusals(t *testing.T) {
	d := monthlyDataset(t, 6)
	manySeries := make([]string, MaxChartSeries+1)
	for i := range manySeries {
		manySeries[i] = "human"
	}
	cases := map[string]ChartRequest{
		"unknown type":                 {Type: "sankey", X: "month", Y: []string{"ai"}},
		"unknown x":                    {Type: ChartBar, X: "nope", Y: []string{"ai"}},
		"unknown y":                    {Type: ChartBar, X: "month", Y: []string{"nope"}},
		"no y":                         {Type: ChartBar, X: "month"},
		"text y":                       {Type: ChartBar, X: "month", Y: []string{"month"}},
		"pie with two series":          {Type: ChartPie, X: "month", Y: []string{"human", "ai"}},
		"too many series":              {Type: ChartLine, X: "month", Y: manySeries},
		"radar with too few spokes":    {Type: ChartRadar, X: "month", Y: []string{"ai"}, Limit: 2},
		"mixed units on one bar scale": {Type: ChartBar, X: "month", Y: []string{"human", "frt"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildChart(d, req); !errors.Is(err, ErrInvalidChart) {
				t.Fatalf("err = %v, want ErrInvalidChart", err)
			}
		})
	}
}

func TestBuildChartRefusesMorePointsThanAChartCanShow(t *testing.T) {
	big := NewDataset("big", []Column{{Key: "k", Kind: ColumnText}, {Key: "v", Kind: ColumnNumber}})
	for i := 0; i < MaxCategoryPoints+1; i++ {
		_ = big.AddRow(fmt.Sprint(i), float64(i))
	}
	if _, err := BuildChart(big, ChartRequest{Type: ChartBar, X: "k", Y: []string{"v"}}); !errors.Is(err, ErrInvalidChart) {
		t.Fatalf("err = %v, want the model told to aggregate or limit", err)
	}
	if _, err := BuildChart(big, ChartRequest{Type: ChartBar, X: "k", Y: []string{"v"}, SortBy: "v", Descending: true, Limit: 10}); err != nil {
		t.Fatalf("a limited chart must work: %v", err)
	}
}

func TestInlineDatasetFeedsTheSamePath(t *testing.T) {
	d, err := InlineDataset("Comparativo", []string{"Agosto", "Setembro"}, []InlineSeries{{Label: "Resolvidas", Values: []float64{120, 150}}}, ColumnNumber)
	if err != nil {
		t.Fatal(err)
	}
	c, err := BuildChart(d, ChartRequest{Type: ChartBar, X: InlineCategoryKey, Y: []string{"s1"}})
	if err != nil || len(c.Categories) != 2 || *c.Series[0].Values[1] != 150 {
		t.Fatalf("chart = %+v err %v", c, err)
	}
	if _, err := InlineDataset("x", []string{"a"}, []InlineSeries{{Label: "s", Values: []float64{1, 2}}}, ColumnNumber); !errors.Is(err, ErrInvalidDataset) {
		t.Fatalf("err = %v, want mismatched lengths refused", err)
	}
}
