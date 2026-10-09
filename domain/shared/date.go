package shared

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrDateInvalid = errors.New("the date is not a valid calendar date (YYYY-MM-DD)")

const DateLayout = "2006-01-02"

type Date struct {
	Year  int
	Month time.Month
	Day   int
}

func ParseDate(raw string) (Date, error) {
	parsed, err := time.Parse(DateLayout, strings.TrimSpace(raw))
	if err != nil {
		return Date{}, ErrDateInvalid
	}
	return DateOf(parsed), nil
}

func DateOf(t time.Time) Date {
	year, month, day := t.Date()
	return Date{Year: year, Month: month, Day: day}
}

func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

func (d Date) String() string {
	return d.Time().Format(DateLayout)
}

func (d Date) After(t time.Time) bool {
	return d.Time().After(DateOf(t).Time())
}

func (d Date) YearsAt(now time.Time) int {
	today := DateOf(now)
	years := today.Year - d.Year
	if today.Month < d.Month || (today.Month == d.Month && today.Day < d.Day) {
		years--
	}
	return years
}

func (d Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Date) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ErrDateInvalid
	}
	parsed, err := ParseDate(text)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
