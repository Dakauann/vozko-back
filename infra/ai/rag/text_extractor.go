package rag

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

type TextExtractor struct{}

func NewTextExtractor() *TextExtractor {
	return &TextExtractor{}
}

func (e *TextExtractor) Extract(data []byte, filename string) (string, error) {
	ext := strings.ToLower(fileExtension(filename))

	var result string
	var err error

	switch ext {
	case ".pdf":
		result, err = e.extractPDF(data)
	case ".docx":
		result, err = e.extractDOCX(data)
	case ".xlsx", ".xls":
		result, err = e.extractXLSX(data)
	case ".csv":
		result, err = e.extractCSV(data)
	case ".txt", ".md", ".json", ".html", ".htm":
		result, err = string(data), nil
	default:
		result, err = string(data), nil
	}

	if err != nil {
		return "", err
	}

	return sanitizeUTF8(result), nil
}

func (e *TextExtractor) extractPDF(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}

	var sb strings.Builder
	numPages := reader.NumPage()

	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			log.Printf("[TextExtractor] warning: failed to extract text from PDF page %d: %v", i, err)
			continue
		}

		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(strings.TrimSpace(text))
	}

	result := sb.String()
	if len(result) == 0 {
		return "", fmt.Errorf("PDF contains no extractable text (may be scanned/image-based)")
	}

	log.Printf("[TextExtractor] extracted %d characters from %d-page PDF", len(result), numPages)
	return result, nil
}

func (e *TextExtractor) extractDOCX(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open DOCX: %w", err)
	}

	var docFile *zip.File
	for _, f := range reader.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}

	if docFile == nil {
		return "", fmt.Errorf("DOCX file does not contain word/document.xml")
	}

	rc, err := docFile.Open()
	if err != nil {
		return "", fmt.Errorf("failed to read word/document.xml: %w", err)
	}
	defer rc.Close()

	xmlData, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("failed to read document XML: %w", err)
	}

	text := extractTextFromXML(xmlData)
	if len(text) == 0 {
		return "", fmt.Errorf("DOCX contains no extractable text")
	}

	log.Printf("[TextExtractor] extracted %d characters from DOCX", len(text))
	return text, nil
}

func extractTextFromXML(data []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var sb strings.Builder
	var inParagraph bool
	var paragraphText strings.Builder

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			localName := t.Name.Local
			if localName == "p" {
				inParagraph = true
				paragraphText.Reset()
			}
		case xml.EndElement:
			localName := t.Name.Local
			if localName == "p" && inParagraph {
				pText := strings.TrimSpace(paragraphText.String())
				if pText != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(pText)
				}
				inParagraph = false
			}
		case xml.CharData:
			if inParagraph {
				paragraphText.Write(t)
			}
		}
	}

	return sb.String()
}

func (e *TextExtractor) extractXLSX(data []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to open Excel file: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	sheets := f.GetSheetList()

	for _, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			log.Printf("[TextExtractor] warning: failed to read sheet %s: %v", sheet, err)
			continue
		}
		section := serializeRecords(sheet, rows)
		if section == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(section)
	}

	result := sb.String()
	if len(result) == 0 {
		return "", fmt.Errorf("Excel file contains no data")
	}

	log.Printf("[TextExtractor] extracted %d characters from Excel (%d sheets)", len(result), len(sheets))
	return result, nil
}

func (e *TextExtractor) extractCSV(data []byte) (string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil && len(rows) == 0 {
		// Not valid CSV, fall back to raw text so nothing is lost.
		return string(data), nil
	}
	result := serializeRecords("", rows)
	if result == "" {
		return string(data), nil
	}
	log.Printf("[TextExtractor] extracted %d characters from CSV (%d rows)", len(result), len(rows))
	return result, nil
}

// serializeRecords turns a table (header row + data rows) into one self-describing
// "label: value | label: value" record per data row, records separated by a blank line,
// aligned to the header by column index. An optional section title leads the block.
func serializeRecords(section string, rows [][]string) string {
	clean := make([][]string, 0, len(rows))
	for _, row := range rows {
		hasValue := false
		norm := make([]string, len(row))
		for i, cell := range row {
			cell = strings.Join(strings.Fields(cell), " ")
			norm[i] = cell
			if cell != "" {
				hasValue = true
			}
		}
		if hasValue {
			clean = append(clean, norm)
		}
	}
	if len(clean) == 0 {
		return ""
	}

	var sb strings.Builder
	if strings.TrimSpace(section) != "" {
		sb.WriteString("## ")
		sb.WriteString(strings.TrimSpace(section))
		sb.WriteString("\n\n")
	}

	if len(clean) == 1 {
		sb.WriteString(joinNonEmpty(clean[0], " | "))
		return strings.TrimSpace(sb.String())
	}

	header := clean[0]
	for _, row := range clean[1:] {
		pairs := make([]string, 0, len(row))
		for i, val := range row {
			if val == "" {
				continue
			}
			label := columnLabel(header, i)
			if label != "" {
				pairs = append(pairs, label+": "+val)
			} else {
				pairs = append(pairs, val)
			}
		}
		if len(pairs) == 0 {
			continue
		}
		sb.WriteString(strings.Join(pairs, " | "))
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}

func columnLabel(header []string, i int) string {
	if i < len(header) {
		if h := strings.TrimSpace(header[i]); h != "" {
			return h
		}
	}
	return fmt.Sprintf("Coluna %d", i+1)
}

func joinNonEmpty(cells []string, sep string) string {
	parts := make([]string, 0, len(cells))
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			parts = append(parts, strings.TrimSpace(c))
		}
	}
	return strings.Join(parts, sep)
}

func fileExtension(filename string) string {
	idx := strings.LastIndex(filename, ".")
	if idx < 0 {
		return ""
	}
	return filename[idx:]
}

func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	log.Printf("[TextExtractor] sanitizing %d bytes of non-UTF8 text", len(s))

	var sb strings.Builder
	sb.Grow(len(s) + len(s)/4)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			if size == 0 {
				break
			}

			sb.WriteRune(rune(s[i]))
			i++
			continue
		}
		sb.WriteRune(r)
		i += size
	}

	return strings.ToValidUTF8(sb.String(), "")
}
