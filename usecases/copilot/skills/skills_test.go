package skills

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"vozko/domain/copilot"
)

func TestWhatTheModelReadsNamesNoSkillSource(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sources := regexp.MustCompile(`(?i)\b(anthropic|claude|remotion)\b`)
	for _, s := range lib.All() {
		if found := sources.FindString(s.Name + "\n" + s.Description + "\n" + s.Body); found != "" {
			t.Errorf("%s names %q to the model", s.Name, found)
		}
	}
}

func TestAnAdaptedSkillShipsWithItsLicenseAndNotice(t *testing.T) {
	dirs, err := os.ReadDir("library")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		folder := filepath.Join("library", dir.Name())
		skill, err := os.ReadFile(filepath.Join(folder, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		_, noticeErr := os.Stat(filepath.Join(folder, "NOTICE.md"))
		_, licenseErr := os.Stat(filepath.Join(folder, "LICENSE.txt"))
		licensed := strings.Contains(string(skill), "\nlicense:")
		if (licensed || noticeErr == nil) && (licenseErr != nil || noticeErr != nil) {
			t.Errorf("%s adapts an outside skill but lacks LICENSE.txt or NOTICE.md", dir.Name())
		}
	}
}

func TestTheShippedLibraryLoadsAndEverySkillIsValid(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.All()) == 0 {
		t.Fatal("the library ships no skills")
	}
	for _, s := range lib.All() {
		if err := s.Validate(); err != nil {
			t.Errorf("%s: %v", s.Name, err)
		}
		found, ok := lib.Find(s.Name)
		if !ok || found.Body != s.Body {
			t.Errorf("%s cannot be found by name", s.Name)
		}
	}
}

func TestAnUnknownSkillIsNotFound(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lib.Find("nao-existe"); ok {
		t.Fatal("an unknown skill was found")
	}
}

func TestASkillFileIsParsedFromItsFrontmatter(t *testing.T) {
	files := fstest.MapFS{"library/publico/SKILL.md": {Data: []byte("---\nname: publico\ndescription: Como montar o público\n---\n\nComece amplo.\n")}}
	lib, err := load(files)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := lib.Find("publico")
	if !ok || got.Description != "Como montar o público" || got.Body != "Comece amplo." {
		t.Fatalf("got %+v", got)
	}
}

func TestABrokenLibraryIsRefused(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"no frontmatter": {"library/a/SKILL.md": {Data: []byte("Comece amplo.")}},
		"invalid skill":  {"library/a/SKILL.md": {Data: []byte("---\nname: A B\ndescription: x\n---\nx")}},
		"duplicate name": {
			"library/a/SKILL.md": {Data: []byte("---\nname: a\ndescription: x\n---\nx")},
			"library/b/SKILL.md": {Data: []byte("---\nname: a\ndescription: y\n---\ny")},
		},
		"empty library": {},
	}
	for name, files := range cases {
		if _, err := load(files); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	_, err := load(fstest.MapFS{"library/a/SKILL.md": {Data: []byte("---\nname: a\ndescription: x\n---\nteste " + string(rune(0x2014)) + " x")}})
	if !errors.Is(err, copilot.ErrSkillDash) {
		t.Fatalf("err %v", err)
	}
}

func TestASkillSurfaceIsReadFromItsFrontmatter(t *testing.T) {
	files := fstest.MapFS{"library/estudio/SKILL.md": {Data: []byte("---\nname: estudio\ndescription: d\nsurface: studio\n---\n\nx\n")}}
	lib, err := load(files)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := lib.Find("estudio"); got.Surface != copilot.SkillStudioOnly {
		t.Fatalf("surface = %q", got.Surface)
	}
	if _, err := load(fstest.MapFS{"library/a/SKILL.md": {Data: []byte("---\nname: a\ndescription: d\nsurface: moon\n---\nx")}}); !errors.Is(err, copilot.ErrSkillSurface) {
		t.Fatalf("err %v", err)
	}
}

func TestTheStudioSeesDesignSkillsAndThePanelSeesAttendanceSkills(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioImage}
	panel := copilot.View{}
	for name, want := range map[string][2]bool{
		"design-de-imagem":        {true, false},
		"motion-design":           {true, false},
		"metricas-de-atendimento": {false, true},
		"criativos-que-performam": {true, true},
		"briefing-criativo":       {true, true},
	} {
		skill, ok := lib.Find(name)
		if !ok {
			t.Fatalf("%s is missing", name)
		}
		if skill.OfferedOn(studio) != want[0] || skill.OfferedOn(panel) != want[1] {
			t.Errorf("%s offered on studio %v and panel %v, want %v", name, skill.OfferedOn(studio), skill.OfferedOn(panel), want)
		}
	}
}

func TestASkillOnlyPointsToSkillsOfferedWhereverItIsOffered(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	views := []copilot.View{{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioVideo}, {}}
	for _, from := range lib.All() {
		for _, to := range lib.All() {
			if to.Name == from.Name || !regexp.MustCompile(`\b`+regexp.QuoteMeta(to.Name)+`\b`).MatchString(from.Description+"\n"+from.Body) {
				continue
			}
			for _, view := range views {
				if from.OfferedOn(view) && !to.OfferedOn(view) {
					t.Errorf("%s points to %s, which is not offered on %q", from.Name, to.Name, view.Surface)
				}
			}
		}
	}
}

func TestArtDirectionPlansWhichAssetsAreGeneratedWithAI(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, ok := lib.Find("direcao-de-arte")
	if !ok {
		t.Fatal("direcao-de-arte must ship")
	}
	for _, step := range []string{"studio_generate_image", "kind cutout", "reference_media_ids"} {
		if !strings.Contains(skill.Body, step) {
			t.Errorf("art direction must teach generating illustrated assets with AI, missing %q", step)
		}
	}
}

func TestVignettesAndLightLeaksComeFromRealEffectsNotRadialShapes(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, ok := lib.Find("profundidade-e-camadas")
	if !ok {
		t.Fatal("profundidade-e-camadas must ship")
	}
	for _, effect := range []string{"add_vfx vignette", "add_vfx lightLeak"} {
		if !strings.Contains(skill.Body, effect) {
			t.Errorf("the depth skill must point to %q instead of a radial shape", effect)
		}
	}
}

func TestDepthTeachesDistanceInLandscapes(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, _ := lib.Find("profundidade-e-camadas")
	for _, rule := range []string{"## Distância e perspectiva", "horizonte", "reflexo", "ponto de fuga"} {
		if !strings.Contains(skill.Body, rule) {
			t.Errorf("the depth skill must teach receding space, missing %q", rule)
		}
	}
}

func TestTypographyKeepsTheBriefVoiceAndCleanDarkLetters(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, _ := lib.Find("tipografia-expressiva")
	for _, rule := range []string{"o brief vence", "Texto escuro sobre fundo claro"} {
		if !strings.Contains(skill.Body, rule) {
			t.Errorf("the typography skill is missing %q", rule)
		}
	}
}
