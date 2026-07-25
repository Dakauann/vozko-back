package media_infra

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"vozko/domain/media"
)

type XLSXParser struct{}

func NewXLSXParser() *XLSXParser { return &XLSXParser{} }

func (p *XLSXParser) SupportedMimeTypes() []string {
	return []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}
}

func (p *XLSXParser) ParseDocument(data []byte, mimeType string) (*media.DocumentContent, error) {
	mime := normalizeMime(mimeType)
	if mime != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		return nil, nil
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open XLSX archive: %w", err)
	}

	sharedStrings, _ := readSharedStrings(zr)

	var sb strings.Builder
	sheetCount := 0
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}

		text, err := readSheet(f, sharedStrings)
		if err != nil {
			continue
		}
		if text != "" {
			sheetCount++
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(fmt.Sprintf("[Planilha %d]\n", sheetCount))
			sb.WriteString(text)
		}

		if sb.Len() > maxExtractedTextLen {
			break
		}
	}

	return &media.DocumentContent{
		Text:      sb.String(),
		PageCount: sheetCount,
		Format:    "xlsx",
	}, nil
}

type xlsxSST struct {
	Items []xlsxSI `xml:"si"`
}

type xlsxSI struct {
	T    string  `xml:"t"`
	Runs []xlsxR `xml:"r"`
}

type xlsxR struct {
	T string `xml:"t"`
}

type xlsxSheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Type  string `xml:"t,attr"`
	Value string `xml:"v"`
}

func readSharedStrings(zr *zip.Reader) ([]string, error) {
	for _, f := range zr.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()

		xmlData, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}

		var sst xlsxSST
		if err := xml.Unmarshal(xmlData, &sst); err != nil {
			return nil, err
		}

		result := make([]string, len(sst.Items))
		for i, si := range sst.Items {
			if si.T != "" {
				result[i] = si.T
			} else {
				var parts []string
				for _, r := range si.Runs {
					parts = append(parts, r.T)
				}
				result[i] = strings.Join(parts, "")
			}
		}
		return result, nil
	}
	return nil, nil
}

func readSheet(f *zip.File, sharedStrings []string) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	xmlData, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	var sheet xlsxSheet
	if err := xml.Unmarshal(xmlData, &sheet); err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, row := range sheet.Rows {
		var cells []string
		for _, cell := range row.Cells {
			val := cell.Value
			if cell.Type == "s" && sharedStrings != nil {

				idx := 0
				fmt.Sscanf(val, "%d", &idx)
				if idx >= 0 && idx < len(sharedStrings) {
					val = sharedStrings[idx]
				}
			}
			cells = append(cells, strings.TrimSpace(val))
		}
		line := strings.Join(cells, "\t")
		if strings.TrimSpace(line) != "" {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

var _ media.DocumentParser = (*XLSXParser)(nil)
