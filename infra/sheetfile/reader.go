package sheetfile

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"vozko/domain/sheet"
)

var ErrUnreadable = errors.New("sheet file: the workbook could not be read")

var zipMagic = []byte("PK\x03\x04")

type Reader struct{}

func (Reader) Read(data []byte) ([]sheet.Row, error) {
	if !bytes.HasPrefix(data, zipMagic) {
		return sheet.Parse(data), nil
	}
	return readWorkbook(data)
}

func readWorkbook(data []byte) ([]sheet.Row, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, ErrUnreadable
	}
	rows, err := f.Rows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	defer rows.Close()
	var out []sheet.Row
	for line := 1; rows.Next(); line++ {
		cells, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
		}
		if blank(cells) {
			continue
		}
		for i, c := range cells {
			cells[i] = strings.TrimSpace(c)
		}
		out = append(out, sheet.Row{Line: line, Cells: cells})
	}
	if err := rows.Error(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return out, nil
}

func blank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
