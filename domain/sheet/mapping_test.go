package sheet

import (
	"errors"
	"reflect"
	"testing"
)

func TestHeaderKey(t *testing.T) {
	cases := []struct {
		header, want string
	}{
		{"  Número  ", "numero"},
		{"Data de Nascimento", "data de nascimento"},
		{"e-mail", "e mail"},
		{"Telefone_2", "telefone 2"},
		{"FONE (Casa)", "fone casa"},
		{"", ""},
		{"---", ""},
	}
	for _, tc := range cases {
		if got := HeaderKey(tc.header); got != tc.want {
			t.Errorf("HeaderKey(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestGuess(t *testing.T) {
	phone := Target{Field: "phone", Aliases: []string{"telefone", "celular"}, Group: "phones"}
	targets := []Target{
		{Field: "number", Aliases: []string{"whatsapp", "telefone"}},
		phone,
		{Field: "name", Aliases: []string{"nome", "nome completo"}},
	}
	cases := []struct {
		name     string
		headers  []string
		capacity map[string]int
		want     []string
	}{
		{
			name:    "aliases fold accents, case and spacing",
			headers: []string{" NOME ", "WhatsApp"},
			want:    []string{"name", "number"},
		},
		{
			name:    "a target without a group takes one column",
			headers: []string{"nome", "nome completo"},
			want:    []string{"name", ""},
		},
		{
			name:    "the next target with room takes a repeated alias",
			headers: []string{"telefone", "telefone"},
			want:    []string{"number", "phone"},
		},
		{
			name:     "a group shares its capacity across columns",
			headers:  []string{"whatsapp", "telefone", "celular", "celular"},
			capacity: map[string]int{"phones": 2},
			want:     []string{"number", "phone", "phone", ""},
		},
		{
			name:     "an exact header wins over a numbered one wherever it sits",
			headers:  []string{"telefone 2", "telefone"},
			capacity: map[string]int{"phones": 4},
			want:     []string{"phone", "number"},
		},
		{
			name:    "a trailing number still finds its alias",
			headers: []string{"nome", "whatsapp2"},
			want:    []string{"name", "number"},
		},
		{
			name:    "unknown headers stay unmapped",
			headers: []string{"cupom", ""},
			want:    []string{"", ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Guess(tc.headers, targets, tc.capacity); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Guess(%q) = %q, want %q", tc.headers, got, tc.want)
			}
		})
	}
}

func TestColumnIndex(t *testing.T) {
	headers := []string{"Nome", " Telefone ", "cidade"}
	cases := []struct {
		name string
		want int
	}{
		{"telefone", 1},
		{" NOME", 0},
		{"", -1},
		{"bairro", -1},
	}
	for _, tc := range cases {
		if got := ColumnIndex(headers, tc.name); got != tc.want {
			t.Errorf("ColumnIndex(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestEachStreamsTheRowsParseReturns(t *testing.T) {
	data := []byte("\xEF\xBB\xBFnumero;nome\n\n5584994409624;Maria\n5584994409625;\"Ana; Paula\"\n")
	var streamed []Row
	if err := Each(data, func(r Row) error {
		streamed = append(streamed, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(streamed, Parse(data)) {
		t.Fatalf("streamed = %+v, parsed = %+v", streamed, Parse(data))
	}
}

func TestEachStopsAtTheFirstError(t *testing.T) {
	stop := errors.New("stop")
	seen := 0
	err := Each([]byte("a\nb\nc\n"), func(Row) error {
		seen++
		if seen == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || seen != 2 {
		t.Fatalf("err = %v, seen = %d", err, seen)
	}
}
