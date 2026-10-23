package googlesheet

import (
	"fmt"
	"time"
)

var serialEpoch = time.Date(1899, time.December, 30, 0, 0, 0, 0, time.UTC)

func (s *Sheet) EncodeRow(rowID string, cells Cells) ([]any, error) {
	row := make([]any, 0, len(s.Columns)+1)
	row = append(row, rowID)
	for _, col := range s.Columns {
		value, ok := cells[col.Key]
		if !ok || value == nil || col.Archived {
			row = append(row, "")
			continue
		}
		encoded, err := s.encodeCell(col, value)
		if err != nil {
			return nil, &ValueError{Column: col.Key, Header: col.Header, Reason: ErrInvalidValue, Detail: err.Error()}
		}
		row = append(row, encoded)
	}
	return row, nil
}

func (s *Sheet) encodeCell(col Column, value any) (any, error) {
	switch col.Type {
	case TypeNumber, TypeMoney:
		number, ok := value.(float64)
		if !ok {
			return nil, fmt.Errorf("stored %T is not a number", value)
		}
		return number, nil
	case TypeBoolean:
		b, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("stored %T is not a boolean", value)
		}
		return b, nil
	case TypeDate:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("stored %T is not a date", value)
		}
		return serialOfDate(text)
	case TypeDateTime:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("stored %T is not a date and time", value)
		}
		at, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, err
		}
		return serialOfClock(at.In(s.Location())), nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("stored %T is not text", value)
	}
	return text, nil
}

func serialOfDate(text string) (float64, error) {
	day, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return 0, err
	}
	return day.Sub(serialEpoch).Hours() / 24, nil
}

func serialOfClock(at time.Time) float64 {
	wall := time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), at.Minute(), at.Second(), 0, time.UTC)
	return wall.Sub(serialEpoch).Hours() / 24
}
