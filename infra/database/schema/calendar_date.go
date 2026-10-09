package schema

import (
	"database/sql/driver"
	"fmt"
	"time"
)

const calendarDateLayout = "2006-01-02"

type CalendarDate string

func (d CalendarDate) Value() (driver.Value, error) {
	if d == "" {
		return nil, nil
	}
	return string(d), nil
}

func (d *CalendarDate) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		*d = ""
		return nil
	case time.Time:
		*d = CalendarDate(v.Format(calendarDateLayout))
		return nil
	case string:
		return d.parse(v)
	case []byte:
		return d.parse(string(v))
	}
	return fmt.Errorf("failed to scan CalendarDate: unsupported type %T", value)
}

func (d *CalendarDate) parse(text string) error {
	parsed, err := time.Parse(calendarDateLayout, text)
	if err != nil {
		return fmt.Errorf("failed to scan CalendarDate: %w", err)
	}
	*d = CalendarDate(parsed.Format(calendarDateLayout))
	return nil
}
