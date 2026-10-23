package emaildesign

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	issueUnknownMergeTag   = "unknown_merge_tag"
	issueMergeTagMalformed = "merge_tag_malformed"
	maxFallbackRunes       = 100
)

type Personalization struct {
	FirstName string
	Name      string
	Email     string
	Company   string
	Fields    map[string]string
	Fallbacks map[string]string
}

type mergeTag struct {
	name        string
	fieldKey    string
	fallback    string
	hasFallback bool
}

type segment struct {
	literal string
	tag     *mergeTag
}

var fieldKeyPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var namedTags = map[string]bool{"first_name": true, "name": true, "email": true, "company": true}

func parseTemplate(s string) ([]segment, []string) {
	var segments []segment
	var problems []string
	rest := s
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			if rest != "" {
				segments = append(segments, segment{literal: rest})
			}
			return segments, problems
		}
		if open > 0 {
			segments = append(segments, segment{literal: rest[:open]})
		}
		after := rest[open+2:]
		end := strings.Index(after, "}}")
		if end < 0 {
			return segments, appendProblem(problems, issueMergeTagMalformed)
		}
		inner := after[:end]
		rest = after[end+2:]
		tag, problem := parseTag(inner)
		if problem != "" {
			problems = appendProblem(problems, problem)
			continue
		}
		segments = append(segments, segment{tag: tag})
	}
}

func appendProblem(problems []string, code string) []string {
	for _, existing := range problems {
		if existing == code {
			return problems
		}
	}
	return append(problems, code)
}

func parseTag(inner string) (*mergeTag, string) {
	if strings.ContainsAny(inner, "{}\r\n") {
		return nil, issueMergeTagMalformed
	}
	name, fallback, hasFallback := strings.Cut(inner, "|")
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, issueMergeTagMalformed
	}
	if hasFallback && (strings.Contains(fallback, "|") || utf8.RuneCountInString(fallback) > maxFallbackRunes) {
		return nil, issueMergeTagMalformed
	}
	tag := &mergeTag{name: name, fallback: fallback, hasFallback: hasFallback}
	if namedTags[name] {
		return tag, ""
	}
	key, isField := strings.CutPrefix(name, "field.")
	if isField && fieldKeyPattern.MatchString(key) {
		tag.fieldKey = key
		return tag, ""
	}
	return nil, issueUnknownMergeTag
}

func (t *mergeTag) resolve(p Personalization) string {
	if value := t.raw(p); strings.TrimSpace(value) != "" {
		return value
	}
	if override, ok := p.Fallbacks[t.name]; ok {
		return override
	}
	return t.fallback
}

func (t *mergeTag) raw(p Personalization) string {
	value := ""
	switch {
	case t.fieldKey != "":
		value = p.Fields[t.fieldKey]
	case t.name == "first_name":
		value = p.FirstName
	case t.name == "name":
		value = p.Name
	case t.name == "email":
		value = p.Email
	case t.name == "company":
		value = p.Company
	}
	return value
}

func mergeProblems(s string) []string {
	_, problems := parseTemplate(s)
	return problems
}

type expander struct {
	person Personalization
}

func (e expander) expand(s string, literal, value func(string) string) (string, error) {
	segments, problems := parseTemplate(s)
	if len(problems) > 0 {
		return "", &ValidationError{Kind: ErrInvalidDocument, Issues: []Issue{{Code: problems[0]}}}
	}
	var b strings.Builder
	for _, seg := range segments {
		if seg.tag == nil {
			b.WriteString(literal(seg.literal))
			continue
		}
		b.WriteString(value(seg.tag.resolve(e.person)))
	}
	return b.String(), nil
}

func identity(s string) string { return s }

func singleLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func stripMergeTags(s string) string {
	segments, _ := parseTemplate(s)
	var b strings.Builder
	for _, seg := range segments {
		b.WriteString(seg.literal)
	}
	return b.String()
}

type MergeTag struct {
	Name     string
	Fallback string
}

func MergeTags(subject string, doc Document) []MergeTag {
	texts := []string{subject}
	if encoded, err := json.Marshal(doc); err == nil {
		var tree any
		if json.Unmarshal(encoded, &tree) == nil {
			texts = collectStrings(tree, texts)
		}
	}
	var tags []MergeTag
	index := map[string]int{}
	for _, text := range texts {
		segments, _ := parseTemplate(text)
		for _, seg := range segments {
			if seg.tag == nil {
				continue
			}
			at, seen := index[seg.tag.name]
			if !seen {
				index[seg.tag.name] = len(tags)
				tags = append(tags, MergeTag{Name: seg.tag.name, Fallback: seg.tag.fallback})
				continue
			}
			if tags[at].Fallback == "" {
				tags[at].Fallback = seg.tag.fallback
			}
		}
	}
	return tags
}

func collectStrings(node any, into []string) []string {
	switch v := node.(type) {
	case string:
		if strings.Contains(v, "{{") {
			into = append(into, v)
		}
	case []any:
		for _, item := range v {
			into = collectStrings(item, into)
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			into = collectStrings(v[key], into)
		}
	}
	return into
}

func (p Personalization) Value(tag string) string {
	parsed, problem := parseTag(tag)
	if problem != "" {
		return ""
	}
	return strings.TrimSpace(parsed.raw(p))
}
