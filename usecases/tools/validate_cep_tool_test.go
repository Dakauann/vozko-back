package tools_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/cep"
)

type fakeCEPSearch struct {
	info  *cep.CEPInfo
	err   error
	codes []string
}

func (f *fakeCEPSearch) Execute(_ context.Context, code string) (*cep.CEPInfo, error) {
	f.codes = append(f.codes, code)
	return f.info, f.err
}

func runValidateCEP(t *testing.T, search cep.CEPSearchUseCase, params map[string]interface{}) (map[string]interface{}, bool) {
	t.Helper()
	result, err := NewValidateCEPToolUseCase(search).Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := result.Result.(map[string]interface{})
	return body, result.IsError
}

func TestValidateCEPAnswersTheAddressFromTheSharedLookup(t *testing.T) {
	search := &fakeCEPSearch{info: &cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"}}
	body, isError := runValidateCEP(t, search, map[string]interface{}{"cep": " 01310-100 "})
	if isError {
		t.Fatalf("unexpected tool error: %v", body)
	}
	if len(search.codes) != 1 || search.codes[0] != "01310100" {
		t.Fatalf("expected one lookup of the parsed CEP, got %v", search.codes)
	}
	if body["isValid"] != true || body["normalizedCEP"] != "01310-100" {
		t.Fatalf("unexpected body %v", body)
	}
	expected := map[string]interface{}{
		"cep": "01310-100", "logradouro": "Avenida Paulista", "complemento": "",
		"bairro": "Bela Vista", "localidade": "São Paulo", "uf": "SP", "ibge": "3550308",
	}
	if !reflect.DeepEqual(body["address"], expected) {
		t.Fatalf("address = %v, expected %v", body["address"], expected)
	}
}

func TestValidateCEPRefusesABadFormatWithoutALookup(t *testing.T) {
	search := &fakeCEPSearch{}
	body, isError := runValidateCEP(t, search, map[string]interface{}{"cep": "CEP 0131"})
	if isError || body["isValid"] != false {
		t.Fatalf("expected an invalid format answer, got %v", body)
	}
	if len(search.codes) != 0 {
		t.Fatalf("an invalid CEP must not be looked up, got %v", search.codes)
	}
}

func TestValidateCEPExplainsLookupFailures(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		found interface{}
	}{
		{"an unknown CEP", cep.ErrNotFound, false},
		{"the lookup is down", cep.ErrUnavailable, nil},
		{"another failure", errors.New("db down"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, isError := runValidateCEP(t, &fakeCEPSearch{err: tt.err}, map[string]interface{}{"cep": "01310100"})
			if isError || body["isValid"] != true || body["error"] == nil || body["address"] != nil {
				t.Fatalf("unexpected answer %v", body)
			}
			if body["found"] != tt.found {
				t.Fatalf("found = %v, expected %v", body["found"], tt.found)
			}
		})
	}
}

func TestValidateCEPRefusesWithoutALookup(t *testing.T) {
	if _, isError := runValidateCEP(t, nil, map[string]interface{}{"cep": "01310100"}); !isError {
		t.Fatal("a tool without a lookup must answer an error")
	}
}

func TestValidateCEPRefusesAMissingParameter(t *testing.T) {
	if _, isError := runValidateCEP(t, &fakeCEPSearch{}, map[string]interface{}{"cep": 1310100}); !isError {
		t.Fatal("a non text CEP must answer an error")
	}
}
