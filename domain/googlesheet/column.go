package googlesheet

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	MaxColumnKeyLength  = 40
	MaxHeaderRunes      = 200
	MaxOptionRunes      = 200
	MaxInstructionRunes = 500
	RefHeader           = "ref"
)

type ColumnType string

const (
	TypeText     ColumnType = "text"
	TypeLongText ColumnType = "long_text"
	TypeNumber   ColumnType = "number"
	TypeMoney    ColumnType = "money"
	TypeDate     ColumnType = "date"
	TypeDateTime ColumnType = "datetime"
	TypeBoolean  ColumnType = "boolean"
	TypeChoice   ColumnType = "choice"
	TypeEmail    ColumnType = "email"
	TypePhone    ColumnType = "phone"
	TypeURL      ColumnType = "url"
)

func ColumnTypes() []ColumnType {
	return []ColumnType{TypeText, TypeLongText, TypeNumber, TypeMoney, TypeDate, TypeDateTime, TypeBoolean, TypeChoice, TypeEmail, TypePhone, TypeURL}
}

func (t ColumnType) Valid() bool {
	for _, known := range ColumnTypes() {
		if t == known {
			return true
		}
	}
	return false
}

type Source string

const (
	SourceInput            Source = "input"
	SourceWrittenAt        Source = "written_at"
	SourceLeadName         Source = "lead_name"
	SourceLeadPhone        Source = "lead_phone"
	SourceLeadEmail        Source = "lead_email"
	SourceConversationLink Source = "conversation_link"
	SourceOrigin           Source = "origin"
)

var systemSourceTypes = map[Source]ColumnType{
	SourceWrittenAt:        TypeDateTime,
	SourceLeadName:         TypeText,
	SourceLeadPhone:        TypePhone,
	SourceLeadEmail:        TypeEmail,
	SourceConversationLink: TypeURL,
	SourceOrigin:           TypeText,
}

func SystemSources() []Source {
	return []Source{SourceWrittenAt, SourceLeadName, SourceLeadPhone, SourceLeadEmail, SourceConversationLink, SourceOrigin}
}

func (s Source) Valid() bool {
	if s == SourceInput {
		return true
	}
	_, ok := systemSourceTypes[s]
	return ok
}

func (s Source) IsSystem() bool { return s != SourceInput }

func (s Source) ColumnType() (ColumnType, bool) {
	t, ok := systemSourceTypes[s]
	return t, ok
}

type Column struct {
	Key         string     `json:"key"`
	Header      string     `json:"header"`
	Type        ColumnType `json:"type"`
	Options     []string   `json:"options,omitempty"`
	Required    bool       `json:"required"`
	Instruction string     `json:"instruction,omitempty"`
	Source      Source     `json:"source"`
	Archived    bool       `json:"archived"`
}

func (c Column) TakesInput() bool { return !c.Archived && c.Source == SourceInput }

type ColumnInput struct {
	Header      string
	Type        ColumnType
	Options     []string
	Required    bool
	Instruction string
	Source      Source
}

func newColumn(in ColumnInput, taken []string) (Column, error) {
	col := Column{
		Header:      strings.TrimSpace(in.Header),
		Type:        in.Type,
		Options:     trimmedOptions(in.Options),
		Required:    in.Required,
		Instruction: strings.TrimSpace(in.Instruction),
		Source:      in.Source,
	}
	if col.Source == "" {
		col.Source = SourceInput
	}
	if err := col.validate(); err != nil {
		return Column{}, err
	}
	col.Key = KeyFromHeader(col.Header, taken)
	return col, nil
}

func (c Column) validate() error {
	switch {
	case c.Header == "":
		return invalidSheet("every column needs a header")
	case utf8.RuneCountInString(c.Header) > MaxHeaderRunes:
		return invalidSheet("header %q is longer than %d characters", c.Header, MaxHeaderRunes)
	case !c.Type.Valid():
		return invalidSheet("column %q has unknown type %q", c.Header, c.Type)
	case !c.Source.Valid():
		return invalidSheet("column %q has unknown source %q", c.Header, c.Source)
	case utf8.RuneCountInString(c.Instruction) > MaxInstructionRunes:
		return invalidSheet("the instruction of %q is longer than %d characters", c.Header, MaxInstructionRunes)
	}
	if want, system := c.Source.ColumnType(); system && want != c.Type {
		return invalidSheet("column %q filled with %s must have type %s", c.Header, c.Source, want)
	}
	if c.Type != TypeChoice {
		if len(c.Options) > 0 {
			return invalidSheet("only single choice columns take options (%q)", c.Header)
		}
		return nil
	}
	if len(c.Options) == 0 {
		return invalidSheet("single choice column %q needs options", c.Header)
	}
	seen := map[string]bool{}
	for _, option := range c.Options {
		folded := shared.FoldForMatch(option)
		switch {
		case option == "":
			return invalidSheet("column %q has a blank option", c.Header)
		case utf8.RuneCountInString(option) > MaxOptionRunes:
			return invalidSheet("an option of %q is longer than %d characters", c.Header, MaxOptionRunes)
		case seen[folded]:
			return invalidSheet("column %q repeats the option %q", c.Header, option)
		}
		seen[folded] = true
	}
	return nil
}

func trimmedOptions(options []string) []string {
	if len(options) == 0 {
		return nil
	}
	out := make([]string, len(options))
	for i, option := range options {
		out[i] = strings.TrimSpace(option)
	}
	return out
}

var ordinalFolds = strings.NewReplacer("º", "o", "ª", "a")

func KeyFromHeader(header string, taken []string) string {
	folded := ordinalFolds.Replace(shared.FoldForMatch(header))
	var b strings.Builder
	underscore := false
	for _, r := range folded {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			underscore = false
			continue
		}
		if !underscore && b.Len() > 0 {
			b.WriteByte('_')
			underscore = true
		}
	}
	base := strings.Trim(b.String(), "_")
	switch {
	case base == "":
		base = "coluna"
	case base[0] >= '0' && base[0] <= '9':
		base = "c_" + base
	}
	base = clipKey(base, MaxColumnKeyLength)
	used := map[string]bool{}
	for _, key := range taken {
		used[key] = true
	}
	key := base
	for n := 2; used[key]; n++ {
		suffix := "_" + strconv.Itoa(n)
		key = clipKey(base, MaxColumnKeyLength-len(suffix)) + suffix
	}
	return key
}

func clipKey(key string, max int) string {
	if len(key) <= max {
		return key
	}
	return strings.TrimRight(key[:max], "_")
}
