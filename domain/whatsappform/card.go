package whatsappform

import (
	"encoding/json"
	"sort"
	"strings"
)

const MetadataKey = "form"

type CardKind string

const (
	CardMessage  CardKind = "message"
	CardResponse CardKind = "response"
)

type MessageCard struct {
	Kind     CardKind `json:"kind"`
	FormID   string   `json:"formId"`
	FormName string   `json:"formName"`
	SendID   string   `json:"sendId"`
	CTA      string   `json:"cta"`
}

func NewMessageCard(form Form, send Send, copy SendCopy) MessageCard {
	return MessageCard{Kind: CardMessage, FormID: form.ID, FormName: form.Name, SendID: send.ID, CTA: copy.CTA}
}

type CardFile struct {
	MediaID  string `json:"mediaId,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type CardAnswer struct {
	Key       string     `json:"key"`
	Prompt    string     `json:"prompt"`
	Value     string     `json:"value"`
	Sensitive bool       `json:"sensitive"`
	Files     []CardFile `json:"files,omitempty"`
}

type ResponseCard struct {
	Kind       CardKind     `json:"kind"`
	FormID     string       `json:"formId,omitempty"`
	FormName   string       `json:"formName,omitempty"`
	ResponseID string       `json:"responseId"`
	Match      MatchResult  `json:"match"`
	Answers    []CardAnswer `json:"answers"`
	Changed    []string     `json:"changed,omitempty"`
	Conflicts  []string     `json:"conflicts,omitempty"`
}

func storedMedia(report ApplyReport) map[string]string {
	out := map[string]string{}
	for _, f := range report.Files {
		if f.MediaID != "" {
			out[f.FileID] = f.MediaID
		}
	}
	return out
}

func cardFiles(answer Answer, sensitive bool, media map[string]string) []CardFile {
	files := make([]CardFile, 0, len(answer.Files))
	for _, f := range answer.Files {
		file := CardFile{MediaID: media[f.ID]}
		if !sensitive {
			file.Name, file.MimeType = f.Name, f.MimeType
		}
		files = append(files, file)
	}
	return files
}

func NewResponseCard(doc *Document, formName string, r Response) ResponseCard {
	card := ResponseCard{
		Kind: CardResponse, FormID: r.FormID, FormName: formName, ResponseID: r.ID, Match: r.Match,
		Changed: r.Report.Changed, Conflicts: r.Report.Conflicts, Answers: []CardAnswer{},
	}
	media := storedMedia(r.Report)
	if doc != nil {
		for _, ref := range doc.Fields() {
			answer, ok := r.Answers.Values[ref.Field.Key]
			if !ok {
				continue
			}
			sensitive := ref.Field.IsSensitive()
			card.Answers = append(card.Answers, CardAnswer{
				Key: ref.Field.Key, Prompt: ref.Field.Prompt(), Value: answer.Shown(ref.Field, false),
				Sensitive: sensitive, Files: cardFiles(answer, sensitive, media),
			})
		}
	}
	for _, key := range rawKeys(r.Answers) {
		card.Answers = append(card.Answers, CardAnswer{Key: key, Prompt: key, Value: rawText(r.Answers.Unknown[key])})
	}
	return card
}

func rawKeys(answers Answers) []string {
	keys := make([]string, 0, len(answers.Unknown))
	for key := range answers.Unknown {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func rawText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return strings.TrimSpace(string(raw))
}

func (a Answers) UnknownLines() []string {
	lines := make([]string, 0, len(a.Unknown))
	for _, key := range rawKeys(a) {
		lines = append(lines, key+": "+rawText(a.Unknown[key]))
	}
	return lines
}

func UnmatchedSummary(answers Answers) string {
	return strings.Join(append([]string{"Formulário respondido"}, answers.UnknownLines()...), "\n")
}
