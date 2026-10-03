package advertisinghttp

import (
	"io"

	"vozko/domain/advertising"
	"vozko/domain/report"
)

var reportCSVHeader = []string{
	"ID na Meta", "Nome", "Nível", "Status", "Veiculação", "Moeda",
	"Gasto", "Impressões", "Cliques", "Cliques no link", "Resultados", "Tipo de resultado", "Custo por resultado",
	"CTR (%)", "CPC", "CPM", "Conversas", "Custo por conversa",
	"Leads", "Custo por lead", "Vendas", "Receita", "ROAS",
}

func writeReportCSV(w io.Writer, rep ReportResponse) error {
	rows := make([][]report.CSVCell, 0, len(rep.Rows))
	for _, row := range rep.Rows {
		rows = append(rows, reportCSVRecord(row))
	}
	_, err := io.WriteString(w, report.BuildCSVDocument([]report.CSVSection{{Header: reportCSVHeader, Rows: rows}}, true))
	return err
}

func reportCSVRecord(row RowResponse) []report.CSVCell {
	m, o := row.Metrics, row.Outcome
	money := func(micros int64) report.CSVCell { return advertising.MoneyCSVCell(&micros) }
	return []report.CSVCell{
		report.Text(row.MetaID), report.Text(row.Name), report.Text(row.Level), report.Text(row.Status), report.Text(row.Delivery), report.Text(m.Currency),
		money(m.Spend), report.Int(m.Impressions), report.Int(m.Clicks), report.Int(m.LinkClicks), report.Int(m.Results), report.Text(m.ResultAction),
		advertising.MoneyCSVCell(m.CostPerResult), report.NumberPtr(m.CTR), advertising.MoneyCSVCell(m.CPC), advertising.MoneyCSVCell(m.CPM),
		report.Int(m.Conversations), advertising.MoneyCSVCell(m.CostPerConversation),
		report.Int(o.Leads), advertising.MoneyCSVCell(o.CostPerLead), report.Int(o.WonDeals), money(o.Revenue), report.NumberPtr(o.ROAS),
	}
}
