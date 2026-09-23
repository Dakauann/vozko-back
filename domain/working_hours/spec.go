package working_hours

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata"
)

var (
	ErrUnknownTimezone = errors.New("working hours: unknown timezone")
	ErrUnknownWeekday  = errors.New("working hours: unknown weekday")
	ErrBadTime         = errors.New("working hours: time must be HH:MM")
	ErrBadHoliday      = errors.New("working hours: holiday must be YYYY-MM-DD")
)

const holidayLayout = "2006-01-02"

var wireWeekdays = map[string]time.Weekday{
	"sun": time.Sunday,
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
}

var wireOrder = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

type Window struct {
	Start string `json:"start" example:"09:00"`
	End   string `json:"end" example:"18:00"`
}

type Spec struct {
	Timezone string              `json:"timezone" example:"America/Sao_Paulo"`
	Days     map[string][]Window `json:"days"`
	Holidays []string            `json:"holidays,omitempty"`
}

func (s *Spec) Validate() error {
	if s == nil {
		return nil
	}
	if _, err := s.location(); err != nil {
		return err
	}
	_, err := s.compileDays()
	if err != nil {
		return err
	}
	if _, err := s.compileHolidays(); err != nil {
		return err
	}
	sched, err := s.Compile()
	if err != nil {
		return err
	}
	return sched.Validate()
}

func (s *Spec) Compile() (*Schedule, error) {
	if s == nil {
		return nil, nil
	}
	loc, err := s.location()
	if err != nil {
		return nil, err
	}
	days, err := s.compileDays()
	if err != nil {
		return nil, err
	}
	holidays, err := s.compileHolidays()
	if err != nil {
		return nil, err
	}
	return NewWithHolidays(loc, days, holidays), nil
}

func (s *Spec) compileHolidays() ([]time.Time, error) {
	if len(s.Holidays) == 0 {
		return nil, nil
	}
	loc, err := s.location()
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, len(s.Holidays))
	for _, raw := range s.Holidays {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parsed, err := time.ParseInLocation(holidayLayout, trimmed, loc)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrBadHoliday, raw)
		}
		out = append(out, parsed)
	}
	return out, nil
}

func (s *Spec) Normalized() *Spec {
	if s == nil {
		return nil
	}
	out := &Spec{Timezone: strings.TrimSpace(s.Timezone), Days: map[string][]Window{}}
	for _, key := range wireOrder {
		windows, ok := s.Days[key]
		if !ok || len(windows) == 0 {
			continue
		}
		norm := make([]Window, 0, len(windows))
		for _, w := range windows {
			start, errStart := parseHHMM(w.Start)
			end, errEnd := parseHHMM(w.End)
			if errStart != nil || errEnd != nil {
				norm = append(norm, w)
				continue
			}
			norm = append(norm, Window{Start: formatHHMM(start), End: formatHHMM(end)})
		}
		sort.SliceStable(norm, func(i, j int) bool { return norm[i].Start < norm[j].Start })
		out.Days[key] = norm
	}
	out.Holidays = normalizeHolidays(s.Holidays)
	return out
}

func normalizeHolidays(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func (s *Spec) location() (*time.Location, error) {
	name := strings.TrimSpace(s.Timezone)
	if name == "" {
		return nil, fmt.Errorf("%w: empty", ErrUnknownTimezone)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTimezone, name)
	}
	return loc, nil
}

func (s *Spec) compileDays() (map[time.Weekday][]Interval, error) {
	out := make(map[time.Weekday][]Interval, len(s.Days))
	for key, windows := range s.Days {
		weekday, ok := wireWeekdays[strings.ToLower(strings.TrimSpace(key))]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownWeekday, key)
		}
		intervals := make([]Interval, 0, len(windows))
		for _, w := range windows {
			iv, err := w.interval()
			if err != nil {
				return nil, err
			}
			intervals = append(intervals, iv)
		}
		if len(intervals) > 0 {
			out[weekday] = intervals
		}
	}
	return out, nil
}

func (w Window) interval() (Interval, error) {
	start, err := parseHHMM(w.Start)
	if err != nil {
		return Interval{}, err
	}
	end, err := parseHHMM(w.End)
	if err != nil {
		return Interval{}, err
	}
	if start >= MinutesPerDay {
		return Interval{}, fmt.Errorf("%w: start %q", ErrIntervalOutOfRange, w.Start)
	}
	if end == start {
		return Interval{}, fmt.Errorf("%w: %q-%q", ErrIntervalEmpty, w.Start, w.End)
	}
	if end < start {
		end += MinutesPerDay
	}
	return Interval{StartMin: start, EndMin: end}, nil
}

func parseHHMM(v string) (int, error) {
	raw := strings.TrimSpace(v)
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("%w: %q", ErrBadTime, v)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrBadTime, v)
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrBadTime, v)
	}
	if minutes < 0 || minutes > 59 {
		return 0, fmt.Errorf("%w: %q", ErrBadTime, v)
	}
	if hours == 24 && minutes == 0 {
		return MinutesPerDay, nil
	}
	if hours < 0 || hours > 23 {
		return 0, fmt.Errorf("%w: %q", ErrBadTime, v)
	}
	return hours*60 + minutes, nil
}

func formatHHMM(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func DecodeSpec(raw *string) (*Spec, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var spec Spec
	if err := json.Unmarshal([]byte(trimmed), &spec); err != nil {
		return nil, fmt.Errorf("working hours: decoding stored schedule: %w", err)
	}
	return &spec, nil
}

var policyErrors = []error{
	ErrUnknownTimezone,
	ErrUnknownWeekday,
	ErrBadTime,
	ErrIntervalEmpty,
	ErrIntervalOverlap,
	ErrIntervalOutOfRange,
	ErrNoOpenTime,
	ErrNoLocation,
	ErrBadHoliday,
	ErrRangeTooWide,
}

func IsPolicyError(err error) bool {
	for _, sentinel := range policyErrors {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

func DecodePatch(body []byte, field string) (spec *Spec, clear bool, err error) {
	if len(body) == 0 {
		return nil, false, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, nil
	}
	raw, present := envelope[field]
	if !present {
		return nil, false, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, true, nil
	}
	var parsed Spec
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, fmt.Errorf("working hours: %q must be an object or null: %w", field, err)
	}
	return &parsed, false, nil
}

func EncodeSpec(s *Spec) (*string, error) {
	if s == nil {
		return nil, nil
	}
	raw, err := json.Marshal(s.Normalized())
	if err != nil {
		return nil, fmt.Errorf("working hours: encoding schedule: %w", err)
	}
	out := string(raw)
	return &out, nil
}
