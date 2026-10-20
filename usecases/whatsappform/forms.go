package whatsappform_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"vozko/domain/customfield"
	wf "vozko/domain/whatsappform"
)

const copySuffix = " (cópia"

type BindingOption struct {
	Target     wf.BindingTarget `json:"target"`
	FieldKinds []wf.FieldKind   `json:"fieldKinds"`
}

type CustomFieldOption struct {
	Key        string                `json:"key"`
	Label      string                `json:"label"`
	Type       customfield.FieldType `json:"type"`
	Options    []string              `json:"options,omitempty"`
	FieldKinds []wf.FieldKind        `json:"fieldKinds"`
}

type Options struct {
	FieldKinds      []wf.FieldKind      `json:"fieldKinds"`
	DisplayKinds    []wf.DisplayKind    `json:"displayKinds"`
	Categories      []wf.Category       `json:"categories"`
	Limits          wf.Limits           `json:"limits"`
	Bindings        []BindingOption     `json:"bindings"`
	CustomFields    []CustomFieldOption `json:"customFields"`
	Starters        []wf.Starter        `json:"starters"`
	FlowJSONVersion string              `json:"flowJsonVersion"`
}

type FormDetail struct {
	Form         wf.Form          `json:"form"`
	Draft        wf.Version       `json:"draft"`
	Published    *wf.Version      `json:"published,omitempty"`
	Publications []wf.Publication `json:"publications"`
}

type CreateInput struct {
	Name        string
	Description string
	Starter     string
}

var bindingOrder = []wf.BindingTarget{
	wf.BindName, wf.BindEmail, wf.BindBirthDate, wf.BindAddressZip, wf.BindAddressCity, wf.BindAddressState, wf.BindWhatsAppConsent,
}

func (s *Service) Options(ctx context.Context, a Actor) (Options, error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return Options{}, err
	}
	defs, err := s.leadDefinitions(a.WorkspaceID)
	if err != nil {
		return Options{}, err
	}
	kinds := wf.BindableKinds()
	bindings := make([]BindingOption, 0, len(bindingOrder))
	for _, target := range bindingOrder {
		bindings = append(bindings, BindingOption{Target: target, FieldKinds: kinds[target]})
	}
	custom := []CustomFieldOption{}
	for _, d := range defs {
		if d == nil || d.Sensitive || d.ObjectType != customfield.ObjectLead {
			continue
		}
		custom = append(custom, CustomFieldOption{Key: d.Key, Label: d.Label, Type: d.Type, Options: d.Options, FieldKinds: wf.CustomFieldKinds(d.Type)})
	}
	return Options{
		FieldKinds: wf.FieldKinds, DisplayKinds: wf.DisplayKinds, Categories: wf.Categories, Limits: wf.CurrentLimits(),
		Bindings: bindings, CustomFields: custom, Starters: wf.Starters(), FlowJSONVersion: wf.FlowJSONVersion,
	}, nil
}

func (s *Service) problemsOf(ctx context.Context, workspaceID string, doc wf.Document) (wf.Problems, wf.Assets, error) {
	defs, err := s.leadDefinitions(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	assets, err := s.deps.Assets.Load(ctx, workspaceID, doc.MediaIDs())
	if err != nil {
		return nil, nil, fmt.Errorf("images of the form: %w", err)
	}
	problems := wf.Validate(doc, defs)
	problems = append(problems, wf.MissingAssets(doc, assets)...)
	return problems, assets, nil
}

func (s *Service) newVersion(ctx context.Context, a Actor, formID string, number int, doc wf.Document) (wf.Version, error) {
	problems, assets, err := s.problemsOf(ctx, a.WorkspaceID, doc)
	if err != nil {
		return wf.Version{}, err
	}
	v := wf.Version{
		ID: s.deps.NewID(), WorkspaceID: a.WorkspaceID, FormID: formID, Number: number, Document: doc,
		CompilerVersion: wf.CompilerVersion, CreatedBy: a.UserID, CreatedAt: s.now(),
	}
	if len(problems) > 0 {
		v.Problems = problems
		return v, nil
	}
	compiled, err := wf.Compile(doc, assets)
	var compileProblems wf.Problems
	switch {
	case errors.As(err, &compileProblems):
		v.Problems = compileProblems
	case err != nil:
		return wf.Version{}, err
	default:
		v.FlowJSON, v.Hash, v.FlowVersion = compiled.JSON, compiled.Hash, compiled.FlowVersion
	}
	return v, nil
}

func (s *Service) Create(ctx context.Context, a Actor, in CreateInput) (FormDetail, error) {
	if err := s.require(a, wf.CapabilityBuild); err != nil {
		return FormDetail{}, err
	}
	name, err := wf.NormalizeName(in.Name)
	if err != nil {
		return FormDetail{}, err
	}
	description, err := wf.NormalizeDescription(in.Description)
	if err != nil {
		return FormDetail{}, err
	}
	doc := wf.BlankDocument()
	if in.Starter != "" {
		starter, ok := wf.StarterByKey(in.Starter)
		if !ok {
			return FormDetail{}, wf.ErrStarterUnknown
		}
		doc = starter.Document
	}
	return s.createWith(ctx, a, name, description, doc)
}

func (s *Service) createWith(ctx context.Context, a Actor, name, description string, doc wf.Document) (FormDetail, error) {
	now := s.now()
	form := wf.Form{
		ID: s.deps.NewID(), WorkspaceID: a.WorkspaceID, Name: name, Description: description, Kind: wf.KindBuilt,
		CreatedBy: a.UserID, CreatedAt: now, UpdatedAt: now,
	}
	version, err := s.newVersion(ctx, a, form.ID, 1, doc)
	if err != nil {
		return FormDetail{}, err
	}
	form.DraftVersionID = version.ID
	if err := s.deps.Forms.Create(ctx, form, version); err != nil {
		return FormDetail{}, err
	}
	return FormDetail{Form: form, Draft: version, Publications: []wf.Publication{}}, nil
}

func (s *Service) List(ctx context.Context, a Actor, search string, archived bool) ([]wf.FormSummary, error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return nil, err
	}
	return s.deps.Forms.List(ctx, wf.FormQuery{WorkspaceID: a.WorkspaceID, Search: search, IncludeArchived: archived})
}

func (s *Service) detail(ctx context.Context, form *wf.Form) (FormDetail, error) {
	draft, err := s.deps.Versions.Get(ctx, form.WorkspaceID, form.DraftVersionID)
	if err != nil {
		return FormDetail{}, fmt.Errorf("draft of form %s: %w", form.ID, err)
	}
	out := FormDetail{Form: *form, Draft: *draft}
	if form.PublishedVersionID != "" {
		published, err := s.deps.Versions.Get(ctx, form.WorkspaceID, form.PublishedVersionID)
		if err != nil {
			return FormDetail{}, fmt.Errorf("published version of form %s: %w", form.ID, err)
		}
		out.Published = published
	}
	pubs, err := s.deps.Publications.ListForForm(ctx, form.WorkspaceID, form.ID)
	if err != nil {
		return FormDetail{}, err
	}
	out.Publications = append([]wf.Publication{}, pubs...)
	return out, nil
}

func (s *Service) Get(ctx context.Context, a Actor, formID string) (FormDetail, error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return FormDetail{}, err
	}
	form, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID)
	if err != nil {
		return FormDetail{}, err
	}
	return s.detail(ctx, form)
}

func (s *Service) activeForm(ctx context.Context, workspaceID, formID string) (*wf.Form, error) {
	form, err := s.deps.Forms.Get(ctx, workspaceID, formID)
	if err != nil {
		return nil, err
	}
	if err := form.EnsureActive(); err != nil {
		return nil, err
	}
	return form, nil
}

func (s *Service) Rename(ctx context.Context, a Actor, formID, name, description string) (wf.Form, error) {
	if err := s.require(a, wf.CapabilityBuild); err != nil {
		return wf.Form{}, err
	}
	normalized, err := wf.NormalizeName(name)
	if err != nil {
		return wf.Form{}, err
	}
	desc, err := wf.NormalizeDescription(description)
	if err != nil {
		return wf.Form{}, err
	}
	if _, err := s.activeForm(ctx, a.WorkspaceID, formID); err != nil {
		return wf.Form{}, err
	}
	if err := s.deps.Forms.Rename(ctx, a.WorkspaceID, formID, normalized, desc, s.now()); err != nil {
		return wf.Form{}, err
	}
	form, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID)
	if err != nil {
		return wf.Form{}, err
	}
	return *form, nil
}

func sameDocument(a, b wf.Document) bool {
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && string(left) == string(right)
}

func (s *Service) SaveDraft(ctx context.Context, a Actor, formID string, doc wf.Document, baseVersionID string) (wf.Version, error) {
	if err := s.require(a, wf.CapabilityBuild); err != nil {
		return wf.Version{}, err
	}
	form, err := s.activeForm(ctx, a.WorkspaceID, formID)
	if err != nil {
		return wf.Version{}, err
	}
	if form.DraftVersionID != baseVersionID {
		return wf.Version{}, wf.ErrStaleDraft
	}
	current, err := s.deps.Versions.Get(ctx, a.WorkspaceID, form.DraftVersionID)
	if err != nil {
		return wf.Version{}, err
	}
	if sameDocument(current.Document, doc) {
		return *current, nil
	}
	version, err := s.newVersion(ctx, a, formID, current.Number+1, doc)
	if err != nil {
		return wf.Version{}, err
	}
	if err := s.deps.Forms.SaveDraft(ctx, a.WorkspaceID, formID, baseVersionID, version, s.now()); err != nil {
		return wf.Version{}, err
	}
	return version, nil
}

func (s *Service) Check(ctx context.Context, a Actor, doc wf.Document) (wf.Problems, error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return nil, err
	}
	problems, _, err := s.problemsOf(ctx, a.WorkspaceID, doc)
	if err != nil {
		return nil, err
	}
	return append(wf.Problems{}, problems...), nil
}

func (s *Service) Compiled(ctx context.Context, a Actor, formID string) ([]byte, error) {
	detail, err := s.Get(ctx, a, formID)
	if err != nil {
		return nil, err
	}
	if !detail.Draft.Compiled() {
		return nil, detail.Draft.Problems.Err()
	}
	return detail.Draft.FlowJSON, nil
}

func (s *Service) Versions(ctx context.Context, a Actor, formID string) ([]wf.Version, error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return nil, err
	}
	if _, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID); err != nil {
		return nil, err
	}
	return s.deps.Versions.List(ctx, a.WorkspaceID, formID)
}

func copyName(base string, n int) string {
	suffix := copySuffix + ")"
	if n > 1 {
		suffix = copySuffix + " " + strconv.Itoa(n) + ")"
	}
	runes := []rune(base)
	if room := wf.MaxNameLength - utf8.RuneCountInString(suffix); len(runes) > room {
		runes = runes[:room]
	}
	return string(runes) + suffix
}

func (s *Service) Duplicate(ctx context.Context, a Actor, formID string) (FormDetail, error) {
	if err := s.require(a, wf.CapabilityBuild); err != nil {
		return FormDetail{}, err
	}
	source, err := s.Get(ctx, a, formID)
	if err != nil {
		return FormDetail{}, err
	}
	existing, err := s.deps.Forms.List(ctx, wf.FormQuery{WorkspaceID: a.WorkspaceID})
	if err != nil {
		return FormDetail{}, err
	}
	taken := map[string]bool{}
	for _, f := range existing {
		taken[f.Form.Name] = true
	}
	name := copyName(source.Form.Name, 1)
	for n := 2; taken[name]; n++ {
		name = copyName(source.Form.Name, n)
	}
	return s.createWith(ctx, a, name, source.Form.Description, wf.CloneDocument(source.Draft.Document))
}
