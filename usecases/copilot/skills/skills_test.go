package skills

import (
	"errors"
	"testing"
	"testing/fstest"

	"vozko/domain/copilot"
)

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
