package studiotools

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"vozko/domain/studio"
)

type studioTableColumnArgs struct {
	Width *float64 `json:"width,omitempty" desc:"peso da largura da coluna, de 0.01 a 100; as larguras se dividem pelo total, então [2,1,1] dá metade para a primeira"`
	Align string   `json:"align,omitempty" enum:"left,center,right" desc:"alinhamento do texto da coluna; números ficam melhor à direita"`
}

type studioTableStyleArgs struct {
	FontID       string   `json:"font_id,omitempty" desc:"fonte da tabela inteira"`
	FontSize     *float64 `json:"font_size,omitempty" desc:"altura da letra como fração da altura da tabela, de 0.01 a 0.3 (0.06 padrão); quando o texto não cabe, a letra diminui sozinha até caber"`
	LineHeight   *float64 `json:"line_height,omitempty" desc:"altura da linha do texto nas células, de 0.8 a 3"`
	Weight       *int     `json:"weight,omitempty" desc:"peso do texto das células, de 100 a 900"`
	TextColor    string   `json:"text_color,omitempty" desc:"cor do texto das células"`
	Fill         string   `json:"fill,omitempty" desc:"fundo da tabela, #rrggbb ou #rrggbbaa; #00000000 deixa transparente"`
	HeaderFill   string   `json:"header_fill,omitempty" desc:"fundo da linha de cabeçalho"`
	HeaderText   string   `json:"header_text,omitempty" desc:"cor do texto do cabeçalho"`
	HeaderWeight *int     `json:"header_weight,omitempty" desc:"peso do texto do cabeçalho, de 100 a 900"`
	Banding      *bool    `json:"banding,omitempty" desc:"pinta uma linha sim, outra não, com band_fill"`
	BandFill     string   `json:"band_fill,omitempty" desc:"cor das linhas alternadas, #rrggbb ou #rrggbbaa (o alfa no fim deixa translúcida)"`
	Borders      string   `json:"borders,omitempty" enum:"all,rows,outer,none" desc:"bordas: all grade completa, rows só entre as linhas e o contorno, outer só o contorno, none nenhuma"`
	BorderColor  string   `json:"border_color,omitempty" desc:"cor das bordas"`
	BorderWidth  *float64 `json:"border_width,omitempty" desc:"espessura das bordas como fração da altura da tabela, de 0 a 0.05"`
	Padding      *float64 `json:"padding,omitempty" desc:"espaço interno das células em letras, de 0 a 3 (0.6 padrão)"`
}

type studioTableArgs struct {
	TableRows    [][]string              `json:"table_rows,omitempty" desc:"add_table e update_table: o texto de cada célula, uma lista por linha, na ordem; linhas mais curtas ganham células vazias; \\n quebra a linha dentro da célula; update_table troca todo o texto de uma vez e mantém o estilo das células que não mudaram"`
	TableHeader  *bool                   `json:"table_header,omitempty" desc:"add_table e update_table: true faz da primeira linha o cabeçalho (padrão true)"`
	TableColumns []studioTableColumnArgs `json:"table_columns,omitempty" desc:"add_table e update_table: largura e alinhamento de cada coluna, na ordem; colunas omitidas ficam como estão"`
	TableStyle   *studioTableStyleArgs   `json:"table_style,omitempty" desc:"add_table e update_table: a aparência; os campos omitidos ficam como estão"`
	TableRow     *int                    `json:"table_row,omitempty" desc:"update_table com text e style_text: a linha da célula, contando de 0 (o cabeçalho é a linha 0)"`
	TableColumn  *int                    `json:"table_column,omitempty" desc:"update_table com text e style_text: a coluna da célula, contando de 0"`
}

func tableInRange(v *float64, low, high float64) bool { return v == nil || (*v >= low && *v <= high) }

func tableWeight(v *int) bool {
	return v == nil || (*v >= studio.MinTableWeight && *v <= studio.MaxTableWeight && *v%100 == 0)
}

func (a studioTableArgs) argsIssue() string {
	for r, row := range a.TableRows {
		for c, text := range row {
			if utf8.RuneCountInString(text) > studio.MaxTextRunes || strings.ContainsAny(text, "\t\r") {
				return fmt.Sprintf("table_rows[%d][%d] tem até %d caracteres, sem tabulação; use \\n para quebrar a linha", r, c, studio.MaxTextRunes)
			}
		}
	}
	for i, column := range a.TableColumns {
		if column.Align != "" && !slices.Contains(studio.TableAligns(), column.Align) {
			return "table_columns[].align deve ser um de: " + strings.Join(studio.TableAligns(), ", ")
		}
		if !tableInRange(column.Width, studio.MinTableColumnWidth, studio.MaxTableColumnWidth) {
			return fmt.Sprintf("table_columns[%d].width vai de %g a %g", i, studio.MinTableColumnWidth, float64(studio.MaxTableColumnWidth))
		}
	}
	if (a.TableRow != nil && *a.TableRow < 0) || (a.TableColumn != nil && *a.TableColumn < 0) {
		return "table_row e table_column contam de 0"
	}
	return a.TableStyle.issue()
}

func (s *studioTableStyleArgs) issue() string {
	if s == nil {
		return ""
	}
	for _, color := range []string{s.TextColor, s.Fill, s.HeaderFill, s.HeaderText, s.BandFill, s.BorderColor} {
		if color != "" && !chartColor(color) {
			return "as cores da tabela são #rrggbb ou #rrggbbaa"
		}
	}
	switch {
	case s.FontID != "" && !slices.Contains(studio.Fonts(), s.FontID):
		return fmt.Sprintf("table_style.font_id %q não existe", s.FontID)
	case s.Borders != "" && !slices.Contains(studio.TableBorders(), s.Borders):
		return "table_style.borders deve ser um de: " + strings.Join(studio.TableBorders(), ", ")
	case !tableInRange(s.FontSize, studio.MinTableFontSize, studio.MaxTableFontSize):
		return fmt.Sprintf("table_style.font_size vai de %g a %g", studio.MinTableFontSize, studio.MaxTableFontSize)
	case !tableInRange(s.LineHeight, studio.MinTableLineHeight, studio.MaxTableLineHeight):
		return fmt.Sprintf("table_style.line_height vai de %g a %g", studio.MinTableLineHeight, float64(studio.MaxTableLineHeight))
	case !tableInRange(s.BorderWidth, 0, studio.MaxTableBorderWidth):
		return fmt.Sprintf("table_style.border_width vai de 0 a %g", studio.MaxTableBorderWidth)
	case !tableInRange(s.Padding, 0, studio.MaxTablePadding):
		return fmt.Sprintf("table_style.padding vai de 0 a %g", float64(studio.MaxTablePadding))
	case !tableWeight(s.Weight) || !tableWeight(s.HeaderWeight):
		return "table_style.weight e table_style.header_weight vão de 100 a 900, de 100 em 100"
	}
	return ""
}

const studioTableIntro = "Tabelas de verdade, em vez de retângulos e textos soltos: a tabela guarda as células e se desenha sozinha com formas e textos comuns (cabeçalho, linhas alternadas, bordas e alinhamento por coluna), igual na tela e na exportação; a letra diminui sozinha quando o texto não cabe. " +
	"update_table troca só o que vier (table_rows, table_header, table_columns, table_style) e mantém o resto; com table_row, table_column e text troca só o texto daquela célula. " +
	"style_text com table_row e table_column estiliza parte do texto de uma célula (match ou char_start e char_end, e fill, font_id, font_weight, italic, underline, caps, letter_spacing; o tamanho da letra é da tabela inteira); sem match nem posições, a célula inteira. " +
	"studio_read mostra a tabela em table, com rows, columns, style e styled_cells. " +
	"Exemplo: {\"op\":\"add_table\",\"table_rows\":[[\"Plano\",\"Preço\"],[\"Básico\",\"R$ 49\"],[\"Pro\",\"R$ 99\"]],\"table_columns\":[{\"width\":2},{\"align\":\"right\"}]}. "

const studioTableImageGuide = studioTableIntro +
	"add_table(table_rows, table_header?, table_columns?, table_style?, name?, artboard_id?, x?, y?, w?, h?), update_table(layer_id, os campos que mudam). "

const studioTableVideoGuide = studioTableIntro +
	"add_table(table_rows, table_header?, table_columns?, table_style?, at_ms, duration_ms?, track_id?, x?, y?, w?, h?), update_table(clip_id, os campos que mudam). "
