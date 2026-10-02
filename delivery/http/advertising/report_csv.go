package advertisinghttp

import (
	"encoding/csv"
	"io"
	"strconv"

	"vozko/domain/advertising"
)

const utf8BOM = "\xef\xbb\xbf"

var reportCSVHeader = []string{
	"ID na Meta", "Nome", "Nível", "Status", "Veiculação", "Moeda",
	"Gasto", "Impressões", "Cliques", "Cliques no link", "Resultados", "Tipo de resultado", "Custo por resultado",
	"CTR (%)", "CPC", "CPM", "Conversas", "Custo por conversa",
	"Leads", "Custo por lead", "Vendas", "Receita", "ROAS",
}

func writeReportCSV(w io.Writer, report ReportResponse) error {
	if _, err := io.WriteString(w, utf8BOM); err != nil {
		return err
	}
	out := csv.NewWriter(w)
	if err := out.Write(reportCSVHeader); err != nil {
		return err
	}
	for _, row := range report.Rows {
		if err := out.Write(reportCSVRecord(row)); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

func reportCSVRecord(row RowResponse) []string {
	m, o := row.Metrics, row.Outcome
	return []string{
		row.MetaID, row.Name, row.Level, row.Status, row.Delivery, m.Currency,
		money(m.Spend), count(m.Impressions), count(m.Clicks), count(m.LinkClicks), count(m.Results), m.ResultAction, optionalMoney(m.CostPerResult),
		optionalDecimal(m.CTR), optionalMoney(m.CPC), optionalMoney(m.CPM), count(m.Conversations), optionalMoney(m.CostPerConversation),
		count(o.Leads), optionalMoney(o.CostPerLead), count(o.WonDeals), money(o.Revenue), optionalDecimal(o.ROAS),
	}
}

func count(n int64) string { return strconv.FormatInt(n, 10) }

func money(micros int64) string {
	return strconv.FormatFloat(advertising.MicrosToAmount(micros), 'f', 2, 64)
}

func optionalMoney(micros *int64) string {
	if micros == nil {
		return ""
	}
	return money(*micros)
}

func optionalDecimal(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', 2, 64)
}
