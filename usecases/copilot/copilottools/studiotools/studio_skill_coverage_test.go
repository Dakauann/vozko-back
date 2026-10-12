package studiotools

import (
	"regexp"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/usecases/copilot/skills"
)

var undoingPrefixes = []string{"remove_", "delete_", "clear_", "detach_", "reset_", "un"}

var studioReadOutputKeys = map[string]bool{"transition_in": true, "transition_out": true, "motion_report": true, "not_applied": true, "per_artboard": true}

var housekeepingOperations = map[string]bool{"seek": true, "select": true, "set_hidden": true, "set_locked": true}

var studioCapabilityFields = []string{
	"fills", "strokes", "effects", "pattern", "gradient_map", "blend_mode", "path_preset", "curve", "highlight",
	"mask_shape", "vfx_kind", "chroma_preset", "color_grade", "arrow_start_kind", "rotation_x", "repeat", "first_vertex",
	"chart_series", "table_rows", "qr_url", "style_values", "layout_direction", "marks", "region", "focus_id", "view",
}

func studioSkillNames(library *skills.Library, extra ...string) []string {
	names := append([]string(nil), extra...)
	for _, skill := range library.All() {
		if skill.Surface == copilot.SkillStudioOnly {
			names = append(names, skill.Name)
		}
	}
	return names
}

func studioSkillText(t *testing.T) string {
	t.Helper()
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, name := range studioSkillNames(library, "briefing-criativo", "formatos-e-zonas-seguras", "carrossel-e-miniatura", "anuncio-ugc-e-depoimento", "oferta-e-preco", "explicativo-e-dados", "pecas-por-segmento", "video-com-som", "criativos-que-performam") {
		skill, ok := library.Find(name)
		if !ok {
			t.Fatalf("the %s skill is missing", name)
		}
		text.WriteString(skill.Description + "\n" + skill.Body + "\n")
	}
	return text.String()
}

func undoing(op string) bool {
	if housekeepingOperations[op] {
		return true
	}
	for _, prefix := range undoingPrefixes {
		if strings.HasPrefix(op, prefix) {
			return true
		}
	}
	return false
}

func mentions(text, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

func TestEveryStudioOperationIsTaughtBySomeSkill(t *testing.T) {
	text := studioSkillText(t)
	for _, ops := range [][]string{opEnum(studioVideoOperation{}), opEnum(studioImageOperation{})} {
		for _, op := range ops {
			if !undoing(op) && !mentions(text, op) {
				t.Errorf("no studio skill mentions %s; say in the skill that owns the craft when and how to use it", op)
			}
		}
	}
}

func TestEveryStudioCapabilityFieldIsRealAndTaughtBySomeSkill(t *testing.T) {
	known := studioVocabulary()
	text := studioSkillText(t)
	for _, field := range studioCapabilityFields {
		if !known[field] {
			t.Errorf("%s is no longer a studio field; update the capability list", field)
		}
		if !mentions(text, field) {
			t.Errorf("no studio skill mentions %s", field)
		}
	}
}
