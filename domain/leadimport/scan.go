package leadimport

import (
	"bytes"
	"errors"

	"vozko/domain/sheet"
)

const sniffBytes = 8 << 10

var (
	zipMagic   = []byte("PK\x03\x04")
	errStopped = errors.New("lead import: scan stopped")
)

type Scan struct {
	Headers []string
	Sample  [][]string
	Rows    int
}

func ScanFile(data []byte) (Scan, error) {
	if len(data) > MaxFileBytes {
		return Scan{}, ErrFileTooLarge
	}
	sniff := data[:min(len(data), sniffBytes)]
	if bytes.HasPrefix(sniff, zipMagic) || bytes.IndexByte(sniff, 0) >= 0 {
		return Scan{}, ErrUnsupportedFile
	}
	var scan Scan
	var failure error
	err := sheet.Each(data, func(row sheet.Row) error {
		if scan.Headers == nil {
			if len(row.Cells) > MaxHeaderCells {
				failure = ErrUnsupportedFile
				return errStopped
			}
			scan.Headers = row.Cells
			return nil
		}
		scan.Rows++
		if scan.Rows > MaxRows {
			failure = ErrTooManyRows
			return errStopped
		}
		if len(scan.Sample) < SampleRows {
			scan.Sample = append(scan.Sample, row.Cells)
		}
		return nil
	})
	if failure != nil {
		return Scan{}, failure
	}
	if err != nil {
		return Scan{}, err
	}
	if scan.Rows == 0 {
		return Scan{}, ErrFileEmpty
	}
	return scan, nil
}

func EachRow(data []byte, fn func(index int, row sheet.Row) error) error {
	header, index := true, 0
	return sheet.Each(data, func(row sheet.Row) error {
		if header {
			header = false
			return nil
		}
		index++
		return fn(index-1, row)
	})
}
