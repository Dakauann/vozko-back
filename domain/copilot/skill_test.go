package copilot

import (
	"errors"
	"testing"
)

func TestASkillNeedsAKebabNameADescriptionAndABody(t *testing.T) {
	valid := Skill{Name: "publico-e-segmentacao", Description: "Como montar o público", Body: "Comece amplo."}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		skill Skill
		want  error
	}{
		{"spaces in the name", Skill{Name: "Publico Amplo", Description: "x", Body: "x"}, ErrSkillName},
		{"empty name", Skill{Description: "x", Body: "x"}, ErrSkillName},
		{"no description", Skill{Name: "a", Body: "x"}, ErrSkillDescription},
		{"no body", Skill{Name: "a", Description: "x"}, ErrSkillBody},
		{"em dash in the body", Skill{Name: "a", Description: "x", Body: "teste " + string(rune(0x2014)) + " errado"}, ErrSkillDash},
		{"en dash in the description", Skill{Name: "a", Description: "a " + string(rune(0x2013)) + " b", Body: "x"}, ErrSkillDash},
	}
	for _, tc := range cases {
		if err := tc.skill.Validate(); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v", tc.name, err)
		}
	}
}

func TestASkillTitleIsItsFirstHeading(t *testing.T) {
	skill := Skill{Name: "motion-design", Description: "x", Body: "intro\n# Motion design no Estúdio\n\n## Tempos\n"}
	if got := skill.Title(); got != "Motion design no Estúdio" {
		t.Fatalf("title %q", got)
	}
	if got := (Skill{Name: "sem-titulo", Body: "só texto"}).Title(); got != "sem-titulo" {
		t.Fatalf("a skill without a heading falls back to its name, got %q", got)
	}
}
