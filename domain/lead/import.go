package lead

import (
	"errors"
	"strings"
)

const MaxImportRows = 100000

var (
	ErrImportEmpty = errors.New("lead: import has no rows")

	ErrImportTooManyRows = errors.New("lead: too many rows for a single import")
)

type ExistingPolicy string

const (
	PolicyFillEmpty ExistingPolicy = "fill_empty"

	PolicySkip ExistingPolicy = "skip"
)

func (p ExistingPolicy) Valid() bool {
	return p == PolicyFillEmpty || p == PolicySkip
}

func ParseExistingPolicy(value string) (ExistingPolicy, bool) {
	switch ExistingPolicy(strings.TrimSpace(value)) {
	case "":
		return PolicyFillEmpty, true
	case PolicyFillEmpty:
		return PolicyFillEmpty, true
	case PolicySkip:
		return PolicySkip, true
	default:
		return "", false
	}
}

type RejectReason string

const (
	ReasonInvalid RejectReason = "invalid"

	ReasonDuplicate RejectReason = "duplicate"
)

type ImportRow struct {
	Line   int
	Number string
	Name   string
	Age    *int
}

type Rejection struct {
	Line   int          `json:"line"`
	Number string       `json:"number"`
	Reason RejectReason `json:"reason"`
}

type PreparedImport struct {
	Inputs []BulkLeadInput

	LineByNumber map[string]int

	Rejected []Rejection
}

func PrepareImport(rows []ImportRow) PreparedImport {
	prepared := PreparedImport{
		Inputs:       make([]BulkLeadInput, 0, len(rows)),
		LineByNumber: make(map[string]int, len(rows)),
	}

	seen := make(map[string]struct{}, len(rows))

	for _, row := range rows {
		number := NormalizeNumber(NormalizeRawNumber(row.Number))
		if number == "" {
			prepared.Rejected = append(prepared.Rejected, Rejection{
				Line:   row.Line,
				Number: strings.TrimSpace(row.Number),
				Reason: ReasonInvalid,
			})
			continue
		}

		key := number
		if alternate := GetAlternatePhoneFormat(number); alternate != "" {
			if _, ok := seen[alternate]; ok {
				key = alternate
			}
		}

		if _, duplicate := seen[key]; duplicate {
			prepared.Rejected = append(prepared.Rejected, Rejection{
				Line:   row.Line,
				Number: number,
				Reason: ReasonDuplicate,
			})
			continue
		}

		seen[key] = struct{}{}

		name := NormalizeName(row.Name)
		if ValidateName(name) != nil {
			name = ""
		}

		age := row.Age
		if age != nil && (*age <= 0 || *age > 130) {
			age = nil
		}

		prepared.LineByNumber[number] = row.Line
		prepared.Inputs = append(prepared.Inputs, BulkLeadInput{
			Number: number,
			Name:   name,
			Age:    age,
		})
	}

	return prepared
}

type ImportOutcome struct {
	Created int64 `json:"created"`
	Matched int64 `json:"matched"`

	Blocked int64 `json:"blocked"`
}
