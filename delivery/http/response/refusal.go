package response

import (
	"errors"
	"net/http"
)

type Refusal struct {
	Status   int
	Code     string
	Message  string
	Expected map[string]string
}

type ErrorRefusal struct {
	Err     error
	Refusal Refusal
}

func (r Refusal) Expecting(expected map[string]string) Refusal {
	r.Expected = expected
	return r
}

func WriteRefusal(w http.ResponseWriter, r Refusal) {
	WriteErrorWithCode(w, r.Status, r.Code, r.Message, r.Expected)
}

func MatchRefusal(err error, table []ErrorRefusal) (Refusal, bool) {
	for _, entry := range table {
		if errors.Is(err, entry.Err) {
			return entry.Refusal, true
		}
	}
	return Refusal{}, false
}

func WriteErrorRefusal(w http.ResponseWriter, err error, table []ErrorRefusal) bool {
	refusal, ok := MatchRefusal(err, table)
	if ok {
		WriteRefusal(w, refusal)
	}
	return ok
}

func ErrorRefusalsOf(table []ErrorRefusal) []Refusal {
	out := make([]Refusal, len(table))
	for i, entry := range table {
		out[i] = entry.Refusal
	}
	return out
}

func RefusalCodes(groups ...[]Refusal) []string {
	var codes []string
	for _, group := range groups {
		for _, r := range group {
			codes = append(codes, r.Code)
		}
	}
	return codes
}
