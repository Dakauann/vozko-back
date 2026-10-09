package mediagen

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"vozko/domain/media"
)

type Kind string

const (
	KindImage    Kind = "image"
	KindMusic    Kind = "music"
	KindVoice    Kind = "voice"
	KindVideo    Kind = "video"
	KindCutout   Kind = "cutout"
	KindCaptions Kind = "captions"
	KindDenoise  Kind = "denoise"
	KindProxy    Kind = "proxy"
)

type Storage struct {
	Type      media.MediaType
	Folder    string
	Extension string
	Label     string
}

type kindRules struct {
	storage  Storage
	model    bool
	reusable bool
}

var rules = map[Kind]kindRules{
	KindImage:    {storage: Storage{Type: media.MediaTypeProductImage, Folder: "images", Extension: ".jpg", Label: "Imagem gerada com IA"}, model: true},
	KindMusic:    {storage: Storage{Type: media.MediaTypeAudio, Folder: "audio", Extension: ".m4a", Label: "Música gerada com IA"}, model: true},
	KindVoice:    {storage: Storage{Type: media.MediaTypeAudio, Folder: "audio", Extension: ".m4a", Label: "Locução gerada com IA"}, model: true},
	KindVideo:    {storage: Storage{Type: media.MediaTypeProductVideo, Folder: "videos", Extension: ".mp4", Label: "Vídeo do Estúdio"}},
	KindCutout:   {storage: Storage{Type: media.MediaTypeProductImage, Folder: "images", Extension: ".png", Label: "Imagem sem fundo"}},
	KindCaptions: {storage: Storage{Type: media.MediaTypeDocument, Folder: "captions", Extension: ".vtt", Label: "Legendas"}},
	KindDenoise:  {storage: Storage{Type: media.MediaTypeAudio, Folder: "audio", Extension: ".m4a", Label: "Áudio limpo"}},
	KindProxy:    {storage: Storage{Type: media.MediaTypeStudioProxy, Folder: "proxies", Extension: ".mp4", Label: "Prévia de edição"}, reusable: true},
}

var kinds = []Kind{KindImage, KindMusic, KindVoice, KindVideo, KindCutout, KindCaptions, KindDenoise, KindProxy}

func Kinds() []Kind { return append([]Kind(nil), kinds...) }

func GenerationKinds() []Kind {
	var out []Kind
	for _, k := range kinds {
		if k.UsesModel() {
			out = append(out, k)
		}
	}
	return out
}

func ProcessingKinds() []Kind {
	var out []Kind
	for _, k := range kinds {
		if k.Processing() {
			out = append(out, k)
		}
	}
	return out
}

func (k Kind) Known() bool {
	_, ok := rules[k]
	return ok
}

func (k Kind) UsesModel() bool { return rules[k].model }

func (k Kind) Processing() bool { return k.Known() && !k.UsesModel() }

func (k Kind) Reusable() bool { return rules[k].reusable }

func (k Kind) Topic() string {
	if k.Processing() {
		return RenderTopic
	}
	return Topic
}

func (k Kind) Storage() (Storage, bool) {
	r, ok := rules[k]
	return r.storage, ok
}

type Request struct {
	WorkspaceID       string
	Kind              Kind
	Model             string
	Prompt            string
	Aspect            Aspect
	ReferenceMediaIDs []string
	Voice             string
	Video             Timeline
	SourceMediaID     string
	BillingReference  string
}

type Source struct {
	MediaID string
	URL     string
	Type    media.MediaType
}

type Output struct {
	Bytes              []byte
	MIMEType           string
	Model              string
	ProviderCostMicros int64
	CostReported       bool
	GenerationID       string
}

func (o Output) Billable(kind Kind) error {
	if kind.UsesModel() && (!o.CostReported || o.ProviderCostMicros < 0) {
		return ErrCostUnreported
	}
	return nil
}

func (r Request) Validate() error {
	issues, err := r.contentIssues()
	if err != nil {
		return err
	}
	model := strings.TrimSpace(r.Model)
	switch {
	case r.Kind.UsesModel() && model == "":
		issues = append(issues, FieldIssue{Field: FieldModel, Code: CodeRequired})
	case r.Kind.Processing() && model != "":
		issues = append(issues, FieldIssue{Field: FieldModel, Code: CodeUnexpected})
	}
	return issuesError(issues)
}

func (r Request) ValidateContent() error {
	issues, err := r.contentIssues()
	if err != nil {
		return err
	}
	return issuesError(issues)
}

func issuesError(issues []FieldIssue) error {
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func (r Request) contentIssues() ([]FieldIssue, error) {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	switch r.Kind {
	case KindImage:
		return imageIssues(r), nil
	case KindMusic:
		return textIssues(FieldPrompt, r.Prompt, MaxMusicPromptRunes), nil
	case KindVoice:
		return voiceIssues(r), nil
	case KindVideo:
		return videoIssues(r), nil
	case KindCutout, KindCaptions, KindDenoise, KindProxy:
		return sourceIssues(r.SourceMediaID), nil
	case "":
		return []FieldIssue{{Field: FieldKind, Code: CodeRequired}}, nil
	}
	return []FieldIssue{{Field: FieldKind, Code: CodeUnknown}}, nil
}

func sourceIssues(id string) []FieldIssue {
	if strings.TrimSpace(id) == "" {
		return []FieldIssue{{Field: FieldSource, Code: CodeRequired}}
	}
	return nil
}

func textIssues(field, text string, max int) []FieldIssue {
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "":
		return []FieldIssue{{Field: field, Code: CodeRequired}}
	case utf8.RuneCountInString(trimmed) > max:
		return []FieldIssue{{Field: field, Code: CodeTooLong}}
	}
	return nil
}

func (r Request) normalized() Request {
	out := Request{WorkspaceID: strings.TrimSpace(r.WorkspaceID), Kind: r.Kind, Model: strings.TrimSpace(r.Model), BillingReference: strings.TrimSpace(r.BillingReference)}
	switch r.Kind {
	case KindImage:
		out.Prompt, out.Aspect, out.ReferenceMediaIDs = strings.TrimSpace(r.Prompt), r.Aspect, trimmedReferences(r.ReferenceMediaIDs)
	case KindMusic:
		out.Prompt = strings.TrimSpace(r.Prompt)
	case KindVoice:
		out.Prompt, out.Voice = strings.TrimSpace(r.Prompt), strings.TrimSpace(r.Voice)
	case KindVideo:
		out.Aspect, out.Video = r.Aspect, r.Video.normalized()
	case KindCutout, KindCaptions, KindDenoise, KindProxy:
		out.SourceMediaID = strings.TrimSpace(r.SourceMediaID)
	}
	return out
}

type SourceRole struct {
	MediaID  string
	Field    string
	Accepts  []media.MediaType
	Mismatch string
}

func (r SourceRole) Accepted(t media.MediaType) bool {
	return slices.Contains(r.Accepts, t)
}

var (
	imagesOnly     = []media.MediaType{media.MediaTypeProductImage}
	imagesOrVideos = []media.MediaType{media.MediaTypeProductImage, media.MediaTypeProductVideo}
	soundSources   = []media.MediaType{media.MediaTypeAudio, media.MediaTypeProductVideo}
	videosOnly     = []media.MediaType{media.MediaTypeProductVideo}
)

func (r Request) SourceRoles() []SourceRole {
	n := r.normalized()
	var roles []SourceRole
	switch n.Kind {
	case KindImage:
		for _, id := range n.ReferenceMediaIDs {
			roles = append(roles, SourceRole{MediaID: id, Field: FieldReferences, Accepts: imagesOnly, Mismatch: CodeNotImage})
		}
	case KindVideo:
		for _, id := range uniqueIDs(n.Video.Visual) {
			roles = append(roles, SourceRole{MediaID: id, Field: FieldTimeline, Accepts: imagesOrVideos, Mismatch: CodeWrongType})
		}
		for _, id := range uniqueIDs(n.Video.Audio) {
			roles = append(roles, SourceRole{MediaID: id, Field: FieldTimeline, Accepts: soundSources, Mismatch: CodeWrongType})
		}
	case KindCutout:
		roles = append(roles, SourceRole{MediaID: n.SourceMediaID, Field: FieldSource, Accepts: imagesOnly, Mismatch: CodeNotImage})
	case KindCaptions, KindDenoise:
		roles = append(roles, SourceRole{MediaID: n.SourceMediaID, Field: FieldSource, Accepts: soundSources, Mismatch: CodeWrongType})
	case KindProxy:
		roles = append(roles, SourceRole{MediaID: n.SourceMediaID, Field: FieldSource, Accepts: videosOnly, Mismatch: CodeWrongType})
	}
	return roles
}

func uniqueIDs(tracks []Track) []string {
	var ids []string
	for _, track := range tracks {
		for _, c := range track.Clips {
			if !slices.Contains(ids, c.MediaID) {
				ids = append(ids, c.MediaID)
			}
		}
	}
	return ids
}

func (r Request) Fingerprint(requestedBy string) string {
	n := r.normalized()
	parts := []string{n.WorkspaceID, strings.TrimSpace(requestedBy), string(n.Kind), n.Model, normalizePrompt(n.Prompt), string(n.Aspect), n.Voice, n.SourceMediaID}
	parts = appendCounted(parts, n.ReferenceMediaIDs)
	parts = appendCounted(parts, n.Video.fingerprintParts())
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func appendCounted(parts, items []string) []string {
	parts = append(parts, strconv.Itoa(len(items)))
	for _, item := range items {
		parts = append(parts, strconv.Itoa(len(item)), item)
	}
	return parts
}

func normalizePrompt(prompt string) string {
	return strings.Join(strings.Fields(prompt), " ")
}
