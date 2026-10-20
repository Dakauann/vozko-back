package whatsappform

import "strings"

type SourceRef struct {
	Screen string `json:"screen"`
	Block  string `json:"block,omitempty"`
}

type SourceMap map[string]SourceRef

func (m SourceMap) add(path, screen, block string) {
	m[path] = SourceRef{Screen: screen, Block: block}
}

var pointerSeparators = strings.NewReplacer("[", "/", "]", "", ".", "/")

func pointerSegments(path string) []string {
	var segments []string
	for _, part := range strings.Split(pointerSeparators.Replace(path), "/") {
		if part = strings.TrimSpace(part); part != "" && part != "$" {
			segments = append(segments, part)
		}
	}
	return segments
}

func (m SourceMap) Locate(path string) (string, string) {
	segments := pointerSegments(path)
	for n := len(segments); n > 0; n-- {
		if ref, ok := m[strings.Join(segments[:n], "/")]; ok {
			return ref.Screen, ref.Block
		}
	}
	return "", ""
}

type MetaValidationError struct {
	Message string
	Paths   []string
}

func (m SourceMap) Problems(errs []MetaValidationError) Problems {
	problems := make(Problems, 0, len(errs))
	for _, e := range errs {
		screen, block := "", ""
		for _, path := range e.Paths {
			if screen, block = m.Locate(path); screen != "" {
				break
			}
		}
		problems = append(problems, Problem{Screen: screen, Block: block, Code: ProblemMetaValidation, Message: e.Message})
	}
	return problems
}
