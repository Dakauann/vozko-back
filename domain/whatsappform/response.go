package whatsappform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	tokenKey     = "flow_token"
	maskedAnswer = "••••"
	yesAnswer    = "Sim"
	noAnswer     = "Não"
	rangeJoiner  = " a "
	listJoiner   = ", "
	shownDate    = "02/01/2006"
)

const (
	AnswerUnknownOption = "unknown_option"
	AnswerWrongType     = "wrong_type"
	AnswerInvalidDate   = "invalid_date"
	AnswerTooManyFiles  = "too_many_files"
)

type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	SHA256   string `json:"sha256"`
}

type DateRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Answer struct {
	Key       string     `json:"key"`
	Kind      FieldKind  `json:"kind"`
	Text      string     `json:"text,omitempty"`
	OptionIDs []string   `json:"optionIds,omitempty"`
	Checked   *bool      `json:"checked,omitempty"`
	Range     *DateRange `json:"range,omitempty"`
	Files     []File     `json:"files,omitempty"`
	Masked    bool       `json:"masked,omitempty"`
}

type AnswerProblem struct {
	Key  string `json:"key"`
	Code string `json:"code"`
}

type Answers struct {
	Token    string                     `json:"-"`
	Values   map[string]Answer          `json:"values"`
	Unknown  map[string]json.RawMessage `json:"unknown,omitempty" swaggertype:"object"`
	Problems []AnswerProblem            `json:"problems,omitempty"`
}

func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%w: %v", ErrResponseInvalid, err)
	}
	return fields, nil
}

func ReadToken(raw []byte) (string, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return "", err
	}
	var token string
	if json.Unmarshal(fields[tokenKey], &token) != nil {
		return "", nil
	}
	return strings.TrimSpace(token), nil
}

func ParseResponse(doc Document, raw []byte) (Answers, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return Answers{}, err
	}
	answers := Answers{Values: map[string]Answer{}}
	if token, ok := fields[tokenKey]; ok {
		_ = json.Unmarshal(token, &answers.Token)
		delete(fields, tokenKey)
	}
	for key, value := range fields {
		ref, ok := doc.Field(key)
		if !ok {
			if answers.Unknown == nil {
				answers.Unknown = map[string]json.RawMessage{}
			}
			answers.Unknown[key] = value
			continue
		}
		answer, code := parseAnswer(ref.Field, value)
		switch {
		case code != "":
			answers.Problems = append(answers.Problems, AnswerProblem{Key: key, Code: code})
		case answer != nil:
			answers.Values[key] = *answer
		}
	}
	return answers, nil
}

func parseAnswer(f Field, raw json.RawMessage) (*Answer, string) {
	answer := &Answer{Key: f.Key, Kind: f.Kind}
	switch {
	case f.Kind.IsTextInput(), f.Kind == FieldLongText, f.Kind == FieldDate:
		text, ok := scalarText(raw)
		if !ok {
			return nil, AnswerWrongType
		}
		if text = strings.TrimSpace(text); text == "" {
			return nil, ""
		}
		if f.Kind == FieldDate && !validDate(text) {
			return nil, AnswerInvalidDate
		}
		answer.Text = text
	case f.Kind.IsSingleChoice():
		var id string
		if json.Unmarshal(raw, &id) != nil {
			return nil, AnswerWrongType
		}
		if id == "" {
			return nil, ""
		}
		if _, ok := f.Option(id); !ok {
			return nil, AnswerUnknownOption
		}
		answer.OptionIDs = []string{id}
	case f.Kind.IsMultiChoice():
		var ids []string
		if json.Unmarshal(raw, &ids) != nil {
			return nil, AnswerWrongType
		}
		if len(ids) == 0 {
			return nil, ""
		}
		for _, id := range ids {
			if _, ok := f.Option(id); !ok {
				return nil, AnswerUnknownOption
			}
		}
		answer.OptionIDs = ids
	case f.Kind == FieldDateRange:
		var r struct {
			Start string `json:"start-date"`
			End   string `json:"end-date"`
		}
		if json.Unmarshal(raw, &r) != nil {
			return nil, AnswerWrongType
		}
		if !validDate(r.Start) || !validDate(r.End) {
			return nil, AnswerInvalidDate
		}
		answer.Range = &DateRange{Start: r.Start, End: r.End}
	case f.Kind == FieldOptIn:
		var checked bool
		if json.Unmarshal(raw, &checked) != nil {
			return nil, AnswerWrongType
		}
		answer.Checked = &checked
	case f.Kind.IsMedia():
		var files []struct {
			ID       string `json:"id"`
			Name     string `json:"file_name"`
			MimeType string `json:"mime_type"`
			SHA256   string `json:"sha256"`
		}
		if json.Unmarshal(raw, &files) != nil {
			return nil, AnswerWrongType
		}
		if len(files) == 0 {
			return nil, ""
		}
		if len(files) > uploadLimit(f) {
			return nil, AnswerTooManyFiles
		}
		for _, file := range files {
			if strings.TrimSpace(file.ID) == "" {
				return nil, AnswerWrongType
			}
			answer.Files = append(answer.Files, File{ID: file.ID, Name: file.Name, MimeType: file.MimeType, SHA256: file.SHA256})
		}
	}
	return answer, ""
}

func uploadLimit(f Field) int {
	if f.Max > 0 {
		return f.Max
	}
	return max(1, f.Min)
}

func scalarText(raw json.RawMessage) (string, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) == nil {
		return number.String(), true
	}
	return "", false
}

func validDate(s string) bool {
	_, err := time.Parse(isoDate, s)
	return err == nil
}

func showDate(s string) string {
	at, err := time.Parse(isoDate, s)
	if err != nil {
		return s
	}
	return at.Format(shownDate)
}

func (r DateRange) Shown() (string, string) {
	return showDate(r.Start), showDate(r.End)
}

func (a Answer) Display(f Field) string {
	switch {
	case len(a.OptionIDs) > 0:
		titles := make([]string, 0, len(a.OptionIDs))
		for _, id := range a.OptionIDs {
			if o, ok := f.Option(id); ok {
				titles = append(titles, o.Title)
			}
		}
		return strings.Join(titles, listJoiner)
	case a.Checked != nil:
		if *a.Checked {
			return yesAnswer
		}
		return noAnswer
	case a.Range != nil:
		start, end := a.Range.Shown()
		return start + rangeJoiner + end
	case len(a.Files) > 0:
		names := make([]string, 0, len(a.Files))
		for _, file := range a.Files {
			names = append(names, file.Name)
		}
		return strings.Join(names, listJoiner)
	case a.Kind == FieldDate:
		return showDate(a.Text)
	}
	return a.Text
}

func (f Field) Prompt() string {
	if !blank(f.Question) {
		return strings.TrimSpace(f.Question)
	}
	return strings.TrimSpace(f.Label)
}

func (a Answer) Shown(f Field, reveal bool) string {
	if f.IsSensitive() && !reveal {
		return maskedAnswer
	}
	return a.Display(f)
}

func TextSummary(doc Document, formName string, answers Answers, reveal bool) string {
	lines := []string{fmt.Sprintf("Formulário %q respondido", formName)}
	for _, ref := range doc.Fields() {
		answer, ok := answers.Values[ref.Field.Key]
		if !ok {
			continue
		}
		lines = append(lines, ref.Field.Prompt()+": "+answer.Shown(ref.Field, reveal))
	}
	return strings.Join(lines, "\n")
}

func MaskSensitive(doc Document, answers Answers) Answers {
	out := answers
	out.Values = make(map[string]Answer, len(answers.Values))
	for key, answer := range answers.Values {
		ref, ok := doc.Field(key)
		if !ok || !ref.Field.IsSensitive() {
			out.Values[key] = answer
			continue
		}
		hidden := Answer{Key: answer.Key, Kind: answer.Kind, Text: maskedAnswer, Masked: true}
		for _, f := range answer.Files {
			hidden.Files = append(hidden.Files, File{ID: f.ID})
		}
		out.Values[key] = hidden
	}
	return out
}
