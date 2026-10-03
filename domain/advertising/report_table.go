package advertising

import (
	"sort"
	"strings"
	"time"
)

type ReportCells map[ReportMetric]*float64

type ReportTableRow struct {
	Key        string
	ObjectID   string
	Name       string
	Dimensions []string
	Share      float64
	Values     ReportCells
}

type ReportTable struct {
	View       ReportView
	Breakdowns []Breakdown
	Metrics    []ReportMetric
	Rows       []ReportTableRow
	Totals     ReportCells
}

type ReportDay struct {
	Day    time.Time
	Values ReportCells
}

type DayMetrics struct {
	Day     time.Time
	Metrics Metrics
}

const reportKeySeparator = "\x1f"

type liveTotal struct {
	metrics   Metrics
	thruPlays int64
	single    *LiveMetrics
}

func combineLive(rows []LiveRow) (liveTotal, error) {
	var total liveTotal
	for _, row := range rows {
		sum, err := total.metrics.Add(row.Values.Metrics)
		if err != nil {
			return liveTotal{}, err
		}
		total.metrics = sum
		total.thruPlays += row.Values.Video.ThruPlays
	}
	if len(rows) == 1 {
		total.single = &rows[0].Values
	}
	return total, nil
}

func numberCell(n int64) *float64 {
	v := float64(n)
	return &v
}

func optionalCell(n *int64) *float64 {
	if n == nil {
		return nil
	}
	return numberCell(*n)
}

func metricCell(m Metrics, metric ReportMetric) *float64 {
	switch metric {
	case ReportSpend:
		return numberCell(m.SpendMicros)
	case ReportImpressions:
		return numberCell(m.Impressions)
	case ReportClicks:
		return numberCell(m.Clicks)
	case ReportLinkClicks:
		return numberCell(m.LinkClicks)
	case ReportCTR:
		return m.CTR()
	case ReportCPC:
		return optionalCell(m.CostPerLinkClick())
	case ReportCPM:
		return optionalCell(m.CPM())
	case ReportResults:
		if m.MixedResults || m.ResultAction == "" {
			return nil
		}
		return numberCell(m.Results)
	case ReportCostPerResult:
		return optionalCell(m.CostPerResult())
	case ReportConversations:
		return numberCell(m.Conversations)
	case ReportCostPerConversation:
		return optionalCell(m.CostPerConversation())
	}
	return nil
}

func (t liveTotal) cell(metric ReportMetric) *float64 {
	switch metric {
	case ReportReach:
		if t.single == nil {
			return nil
		}
		return numberCell(t.single.Reach)
	case ReportFrequency:
		if t.single == nil {
			return nil
		}
		v := t.single.Frequency
		return &v
	case ReportThruPlays:
		return numberCell(t.thruPlays)
	case ReportCostPerThruPlay:
		return optionalCell(ratio(t.metrics.SpendMicros, t.thruPlays))
	}
	return metricCell(t.metrics, metric)
}

func (t liveTotal) cells(metrics []ReportMetric) ReportCells {
	out := make(ReportCells, len(metrics))
	for _, metric := range metrics {
		out[metric] = t.cell(metric)
	}
	return out
}

func dimensionsOf(row LiveRow, breakdowns []Breakdown) []string {
	out := make([]string, len(breakdowns))
	for i, b := range breakdowns {
		out[i] = row.Dimensions[b]
	}
	return out
}

func relevantRows(rows []LiveRow, breakdowns []Breakdown) []LiveRow {
	wantsSlices := len(breakdowns) > 0
	out := make([]LiveRow, 0, len(rows))
	for _, row := range rows {
		if (len(row.Dimensions) > 0) == wantsSlices {
			out = append(out, row)
		}
	}
	return out
}

type reportGroup struct {
	key      string
	objectID string
	dims     []string
	rows     []LiveRow
}

func groupRows(rows []LiveRow, byObject bool, breakdowns []Breakdown) []*reportGroup {
	index := map[string]*reportGroup{}
	var groups []*reportGroup
	for _, row := range rows {
		dims := dimensionsOf(row, breakdowns)
		parts := dims
		objectID := ""
		if byObject {
			objectID = row.ObjectID
			parts = append([]string{row.ObjectID}, dims...)
		}
		key := strings.Join(parts, reportKeySeparator)
		group, ok := index[key]
		if !ok {
			group = &reportGroup{key: key, objectID: objectID, dims: dims}
			index[key] = group
			groups = append(groups, group)
		}
		group.rows = append(group.rows, row)
	}
	return groups
}

func BuildReportTable(def ReportDefinition, rows []LiveRow, names map[string]string) (ReportTable, error) {
	relevant := relevantRows(rows, def.Breakdowns)
	byObject := def.View == ViewPivot || len(def.Breakdowns) == 0
	total, err := combineLive(relevant)
	if err != nil {
		return ReportTable{}, err
	}
	table := ReportTable{
		View: def.View, Breakdowns: append([]Breakdown{}, def.Breakdowns...), Metrics: append([]ReportMetric{}, def.Metrics...),
		Rows: []ReportTableRow{}, Totals: total.cells(def.Metrics),
	}
	for _, group := range groupRows(relevant, byObject, def.Breakdowns) {
		sum, err := combineLive(group.rows)
		if err != nil {
			return ReportTable{}, err
		}
		row := ReportTableRow{Key: group.key, ObjectID: group.objectID, Dimensions: group.dims, Values: sum.cells(def.Metrics)}
		if group.objectID != "" {
			row.Name = nameOf(names, group.objectID)
		}
		if total.metrics.SpendMicros > 0 {
			row.Share = float64(sum.metrics.SpendMicros) / float64(total.metrics.SpendMicros)
		}
		table.Rows = append(table.Rows, row)
	}
	sortReportRows(table.Rows, def)
	return table, nil
}

func nameOf(names map[string]string, objectID string) string {
	if name := names[objectID]; name != "" {
		return name
	}
	return objectID
}

func sortReportRows(rows []ReportTableRow, def ReportDefinition) {
	label := func(r ReportTableRow) string {
		return strings.ToLower(r.Name + reportKeySeparator + strings.Join(r.Dimensions, reportKeySeparator))
	}
	if def.View == ViewPivot {
		sort.SliceStable(rows, func(i, j int) bool {
			if a, b := strings.ToLower(rows[i].Name), strings.ToLower(rows[j].Name); a != b {
				return a < b
			}
			if rows[i].ObjectID != rows[j].ObjectID {
				return rows[i].ObjectID < rows[j].ObjectID
			}
			return label(rows[i]) < label(rows[j])
		})
		return
	}
	first := def.Metrics[0]
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Values[first], rows[j].Values[first]
		switch {
		case a != nil && b != nil && *a != *b:
			return *a > *b
		case (a == nil) != (b == nil):
			return a != nil
		}
		return label(rows[i]) < label(rows[j])
	})
}

func BuildReportSeries(metrics []ReportMetric, days []DayMetrics) []ReportDay {
	out := make([]ReportDay, 0, len(days))
	for _, day := range days {
		values := make(ReportCells, len(metrics))
		for _, metric := range metrics {
			values[metric] = metricCell(day.Metrics, metric)
		}
		out = append(out, ReportDay{Day: day.Day, Values: values})
	}
	return out
}
