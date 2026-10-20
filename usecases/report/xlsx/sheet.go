package report_xlsx

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

const defaultSheet = "Sheet1"

var ErrSheetName = errors.New("xlsx: a sheet needs a name")

type Sheet struct {
	file   *excelize.File
	stream *excelize.StreamWriter
	next   int
}

func NewSheet(name string, header []string) (*Sheet, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrSheetName
	}
	file := excelize.NewFile()
	if err := file.SetSheetName(defaultSheet, name); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("xlsx: naming the sheet: %w", err)
	}
	stream, err := file.NewStreamWriter(name)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("xlsx: opening the sheet: %w", err)
	}
	sheet := &Sheet{file: file, stream: stream, next: 1}
	if err := sheet.Append(header); err != nil {
		sheet.Discard()
		return nil, err
	}
	return sheet, nil
}

func (s *Sheet) Append(values []string) error {
	cell, err := excelize.CoordinatesToCellName(1, s.next)
	if err != nil {
		return err
	}
	row := make([]interface{}, len(values))
	for i, value := range values {
		row[i] = value
	}
	if err := s.stream.SetRow(cell, row); err != nil {
		return fmt.Errorf("xlsx: writing row %d: %w", s.next, err)
	}
	s.next++
	return nil
}

func (s *Sheet) Rows() int {
	return s.next - 2
}

func (s *Sheet) Discard() {
	_ = s.file.Close()
}

func (s *Sheet) Write(w io.Writer) error {
	defer s.Discard()
	if err := s.stream.Flush(); err != nil {
		return fmt.Errorf("xlsx: closing the sheet: %w", err)
	}
	return s.file.Write(w)
}

func WriteTable(w io.Writer, name string, header []string, rows [][]string) error {
	sheet, err := NewSheet(name, header)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := sheet.Append(row); err != nil {
			sheet.Discard()
			return err
		}
	}
	return sheet.Write(w)
}
