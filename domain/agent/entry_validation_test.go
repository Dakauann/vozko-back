package agent

import (
	"errors"
	"reflect"
	"testing"
)

func TestMetadataValueMissing(t *testing.T) {
	metadata := map[string]any{"escola": "Prisma", "turma": "  ", "vagas": 0}
	cases := []struct {
		key  string
		want bool
	}{
		{key: "escola", want: false},
		{key: "turma", want: true},
		{key: "vagas", want: false},
		{key: "cidade", want: true},
	}
	for _, tc := range cases {
		if got := MetadataValueMissing(metadata, tc.key); got != tc.want {
			t.Fatalf("MetadataValueMissing(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestMissingEntryMetadataNamesEveryRequiredVariableWithoutAValue(t *testing.T) {
	a := &Agent{Variables: []AgentVariable{{Name: "escola"}, {Name: "turma"}, {Name: "cidade", DefaultValue: "Natal"}}}
	if got := MissingEntryMetadata(a, map[string]any{"escola": "Prisma", "turma": " "}); !reflect.DeepEqual(got, []string{"turma"}) {
		t.Fatalf("a blank variable = %v", got)
	}
	if got := MissingEntryMetadata(a, nil); !reflect.DeepEqual(got, []string{"escola", "turma"}) {
		t.Fatalf("no metadata = %v", got)
	}
	if got := MissingEntryMetadata(a, map[string]any{"escola": "Prisma", "turma": "A"}); len(got) != 0 {
		t.Fatalf("a filled entry = %v", got)
	}
	if got := MissingEntryMetadata(nil, nil); len(got) != 0 {
		t.Fatalf("no agent = %v", got)
	}
}

func TestValidateEntryMetadataRefusesTheFirstMissingVariable(t *testing.T) {
	a := &Agent{Variables: []AgentVariable{{Name: "escola"}}}
	if err := ValidateEntryMetadata(a, map[string]any{"escola": " "}); !errors.Is(err, ErrAgentRequiredVariableMissing) {
		t.Fatalf("a blank variable = %v", err)
	}
	if err := ValidateEntryMetadata(a, map[string]any{"escola": "Prisma"}); err != nil {
		t.Fatalf("a filled variable = %v", err)
	}
}
