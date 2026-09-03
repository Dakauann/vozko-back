package lead

import (
	"errors"
	"strings"
)

// MaxImportRows bounds one import.
//
// A refusal, never a truncation. A silently cut import looks complete: the
// operator sees "10.000 importados", ships a campaign against it, and never
// learns that rows 10.001 onward, the ones they most recently added to the
// spreadsheet, are missing. The number is generous for a hand-managed list and
// still small enough that FindOrCreateMany's 500-row batches stay a bounded
// amount of work inside one request.
const MaxImportRows = 100000

var (
	// ErrImportEmpty means the caller sent no rows at all. A 400: an empty
	// import is a mistake upstream (an unparsed file, a wrong column mapping),
	// not a successful import of nothing.
	ErrImportEmpty = errors.New("lead: import has no rows")

	// ErrImportTooManyRows means the file exceeds MaxImportRows.
	ErrImportTooManyRows = errors.New("lead: too many rows for a single import")
)

// ExistingPolicy decides what an import does with a number the workspace
// already knows.
//
// Never an overwrite. A CRM import is the one operation most likely to be run
// twice by accident (the same file, a re-export with stale names), and a lead
// name is frequently something an operator typed by hand after talking to the
// person. Clobbering that with a spreadsheet's "CLIENTE 4471" is a data loss no
// undo covers.
type ExistingPolicy string

const (
	// PolicyFillEmpty writes name/age ONLY where the stored lead has none.
	// Same rule FindOrCreateMany already applies for campaign uploads, so a
	// list imported here and the same list uploaded to a campaign converge on
	// identical rows.
	PolicyFillEmpty ExistingPolicy = "fill_empty"

	// PolicySkip leaves existing leads completely untouched.
	PolicySkip ExistingPolicy = "skip"
)

func (p ExistingPolicy) Valid() bool {
	return p == PolicyFillEmpty || p == PolicySkip
}

// ParseExistingPolicy resolves a client value, defaulting to fill-empty.
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

// RejectReason says why one row did not make it in.
type RejectReason string

const (
	// ReasonInvalid: the number is not a phone number this product can reach.
	ReasonInvalid RejectReason = "invalid"

	// ReasonDuplicate: the same number appeared earlier in the SAME file.
	// Distinct from a lead that already exists in the workspace, which is not a
	// rejection at all: that row is counted as matched.
	ReasonDuplicate RejectReason = "duplicate"
)

// ImportRow is one line of the operator's file, as typed.
//
// Line is carried through untouched so a rejection can point at the row in the
// spreadsheet still open on the other monitor. Reporting "80 inválidos" without
// saying WHICH 80 leaves an operator with a file they cannot fix.
type ImportRow struct {
	Line   int
	Number string
	Name   string
	Age    *int
}

// Rejection is one row that will not be imported, and why.
type Rejection struct {
	Line   int          `json:"line"`
	Number string       `json:"number"`
	Reason RejectReason `json:"reason"`
}

// PreparedImport is the result of vetting a file: what to write, and what was
// thrown out.
type PreparedImport struct {
	// Inputs are normalized and deduplicated, in file order.
	Inputs []BulkLeadInput

	// LineByNumber maps a normalized number back to the line it came from, so
	// per-number outcomes from the database can be reported against the
	// operator's file rather than against numbers they never typed.
	LineByNumber map[string]int

	Rejected []Rejection
}

// PrepareImport normalizes, validates and deduplicates the rows of an import.
//
// It runs SERVER-SIDE even though the browser has already parsed and checked
// the file. The browser's pass exists to show the operator what will happen
// before they commit; this one exists because the endpoint is reachable without
// it. They apply the same rules, so the preview does not lie about the outcome.
//
// Numbers are read leniently. NormalizeRawNumber accepts the local 10/11-digit
// form every Brazilian spreadsheet is full of and adds the country code, and the
// result is held to the same canonical contract as every other number in the
// product. Rejecting "11987654321" as invalid would fail the most ordinary
// file there is; storing it unnormalized would create a second lead for a
// person the workspace already knows.
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

		// The 12- and 13-digit forms of one Brazilian mobile are the same
		// person. Collapsing them here stops a file that carries both from
		// importing a contact twice, which no later dedupe would undo.
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
			// A name too long for the inbox row is not worth rejecting the
			// contact over: the phone number is the part that makes the lead
			// reachable, so the row lands without the name rather than not at
			// all.
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

// ImportOutcome is what the database did, counted for the operator.
//
// Created and Matched are reported separately because they are different
// events: 500 created is an acquisition, 500 matched is a file that has already
// been imported. Collapsing both into "500 importados" is how an operator ends
// up running the same import four times looking for the leads.
type ImportOutcome struct {
	Created int64 `json:"created"`
	Matched int64 `json:"matched"`

	// Blocked counts matched leads that are blocked in this workspace. They are
	// imported like any other, and the import does not unblock anyone, but a
	// campaign built on this list will not reach them, so the number is said
	// out loud rather than discovered later as silent non-delivery.
	Blocked int64 `json:"blocked"`
}
