package response

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

var (
	errMissing = errors.New("missing")
	errTaken   = errors.New("taken")
	errOther   = errors.New("other")
)

var testRefusals = []ErrorRefusal{
	{Err: errMissing, Refusal: Refusal{Status: http.StatusNotFound, Code: "thing_not_found", Message: "Coisa não encontrada"}},
	{Err: errTaken, Refusal: Refusal{Status: http.StatusConflict, Code: "thing_taken", Message: "Já existe", Expected: map[string]string{"name": "único"}}},
}

func TestWriteErrorRefusal(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		written bool
		body    string
		status  int
	}{
		{"a wrapped mapped error writes its code and status", fmt.Errorf("load: %w", errMissing), true, "{\"error\":true,\"code\":\"thing_not_found\",\"message\":\"Coisa não encontrada\"}\n", http.StatusNotFound},
		{"the expected fields travel with the refusal", errTaken, true, "{\"error\":true,\"code\":\"thing_taken\",\"message\":\"Já existe\",\"expected\":{\"name\":\"único\"}}\n", http.StatusConflict},
		{"an unmapped error writes nothing so the caller decides", errOther, false, "", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if got := WriteErrorRefusal(rec, tc.err, testRefusals); got != tc.written {
				t.Fatalf("written = %v, want %v", got, tc.written)
			}
			if rec.Code != tc.status || rec.Body.String() != tc.body {
				t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRefusalCodesListsEveryCodeOfTheTables(t *testing.T) {
	codes := RefusalCodes(ErrorRefusalsOf(testRefusals), []Refusal{{Code: "invalid_body"}})
	want := []string{"thing_not_found", "thing_taken", "invalid_body"}
	if fmt.Sprint(codes) != fmt.Sprint(want) {
		t.Fatalf("codes = %v, want %v", codes, want)
	}
}

func TestExpectingNamesTheFieldsWithoutChangingTheSharedRefusal(t *testing.T) {
	shared := Refusal{Status: http.StatusBadRequest, Code: "invalid_body", Message: "Corpo inválido"}
	named := shared.Expecting(map[string]string{"revision": "int (required)"})
	if shared.Expected != nil || named.Expected["revision"] != "int (required)" || named.Code != shared.Code {
		t.Fatalf("shared %+v named %+v", shared, named)
	}
}

func TestMatchRefusalFindsTheFirstEntryForAWrappedError(t *testing.T) {
	got, ok := MatchRefusal(fmt.Errorf("x: %w", errTaken), testRefusals)
	if !ok || got.Code != "thing_taken" {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := MatchRefusal(errOther, testRefusals); ok {
		t.Fatal("an unmapped error must not match")
	}
}
