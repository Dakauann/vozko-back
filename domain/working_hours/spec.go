package working_hours

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	// The timezone database is embedded rather than taken from the host.
	//
	// A workspace picks its own IANA zone, so LoadLocation has to work for any
	// of them — on a scratch container with no /usr/share/zoneinfo, and on the
	// Windows boxes this is developed on. Without this import the feature fails
	// exactly where it is least visible: a workspace saves "America/Sao_Paulo",
	// the load fails, and the schedule silently degrades to always-open.
	_ "time/tzdata"
)

var (
	ErrUnknownTimezone = errors.New("working hours: unknown timezone")
	ErrUnknownWeekday  = errors.New("working hours: unknown weekday")
	ErrBadTime         = errors.New("working hours: time must be HH:MM")
)

// wireWeekdays maps the JSON keys to Go weekdays. Short lowercase names rather
// than integers: a stored policy is read by people debugging a distribution
// complaint, and {"mon": ...} says what {"1": ...} does not.
var wireWeekdays = map[string]time.Weekday{
	"sun": time.Sunday,
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
}

// wireOrder is the canonical key order for rendering, so a stored document does
// not reshuffle between writes and diff cleanly in the database.
var wireOrder = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Window is one open period on the wire, as two wall-clock times.
//
// End is exclusive. An End at or before Start runs past midnight into the next
// day — 22:00→02:00 is a night shift, and expressing it that way keeps it one
// row in the UI instead of two halves an admin has to remember to edit
// together. "24:00" is accepted as end-of-day, so a full day is 00:00→24:00.
type Window struct {
	Start string `json:"start" example:"09:00"`
	End   string `json:"end" example:"18:00"`
}

// Spec is the stored, JSON-shaped working-hours policy.
//
// It is deliberately separate from Schedule. Spec is what an admin edits, what
// the API accepts and what sits in a JSONB column: strings, forgiving, easy to
// read in a database console. Schedule is the compiled form the sweep asks
// questions of. Keeping them apart means the evaluation logic never has to
// parse anything, and the wire format can change without touching it.
//
// A nil *Spec means "no working hours configured", which compiles to a nil
// *Schedule and is always open.
type Spec struct {
	Timezone string              `json:"timezone" example:"America/Sao_Paulo"`
	Days     map[string][]Window `json:"days"`
}

// Validate reports whether the spec is storable. It is the API boundary's
// check; Compile repeats the parts it depends on rather than trusting a caller.
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
	sched, err := s.Compile()
	if err != nil {
		return err
	}
	return sched.Validate()
}

// Compile turns the stored spec into the form the roulette evaluates.
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
	return New(loc, days), nil
}

// Normalized returns a copy with canonical key order and zero-padded times, so
// what is stored and echoed back is stable regardless of how it was typed.
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

// interval converts a wire window to minutes from local midnight.
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
		// 24:00 is end-of-day only; as a start it names no minute of the day.
		return Interval{}, fmt.Errorf("%w: start %q", ErrIntervalOutOfRange, w.Start)
	}
	if end == start {
		// Ambiguous between "closed" and "open for a full 24 hours". An admin
		// who means the second writes 00:00-24:00.
		return Interval{}, fmt.Errorf("%w: %q-%q", ErrIntervalEmpty, w.Start, w.End)
	}
	if end < start {
		// Past midnight: expressed on the wire as a smaller end, stored as
		// minutes beyond the day so the whole shift stays one interval.
		end += MinutesPerDay
	}
	return Interval{StartMin: start, EndMin: end}, nil
}

// parseHHMM accepts H:MM and HH:MM, plus the single special case 24:00 for
// end-of-day so a full day can be written 00:00-24:00.
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

// DecodeSpec parses a stored jsonb document.
//
// A NULL or blank column is "no working hours configured" and yields nil with
// no error, which the rest of the system reads as always open. A document that
// is present but unparseable returns an error, so the caller decides what to do
// with it rather than having the choice made here — the repositories log it and
// fall back to always open, which is the behaviour that predates the feature.
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

// policyErrors is every way a submitted schedule can be refused. Listed once so
// the two HTTP handlers that classify these cannot drift apart on which of them
// counts as the caller's mistake.
var policyErrors = []error{
	ErrUnknownTimezone,
	ErrUnknownWeekday,
	ErrBadTime,
	ErrIntervalEmpty,
	ErrIntervalOverlap,
	ErrIntervalOutOfRange,
	ErrNoOpenTime,
	ErrNoLocation,
}

// IsPolicyError reports whether an error is a rejected schedule rather than
// something that broke.
//
// Working hours are refused, never clamped — a window an admin cannot see is a
// window nobody can debug — so these have to reach the client as a 400 naming
// the rule, not as a blanket 500.
func IsPolicyError(err error) bool {
	for _, sentinel := range policyErrors {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// DecodePatch interprets one member of a JSON update body.
//
// A working-hours field has three states and a pointer can only carry two.
// Absent means leave the schedule alone; an explicit null means remove it and
// go back to operating around the clock; a document means replace it. Those are
// different instructions, and the only place they are still distinguishable is
// the raw body — by the time it has been decoded into a struct, absent and null
// look identical.
//
// A body that is not a JSON object returns no patch and no error: the caller's
// own decode of the same body is what reports a malformed request, and
// reporting it twice with different wording helps nobody.
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

// EncodeSpec renders a spec for storage, normalized so repeated writes of the
// same policy produce the same document. A nil spec encodes to a NULL column,
// never to the JSON text "null" — the two look alike in a console and behave
// alike here, but only one of them lets the partial-index and IS NULL checks
// mean what they say.
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
