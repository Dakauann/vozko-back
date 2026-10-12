package tooltest

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
)

type StubImages struct {
	CatalogErr  error
	CheckErr    error
	RequestErr  error
	WaitErr     error
	Final       *mediagen.Job
	Requested   mediagen.Request
	RequestedBy string
	WaitedFor   string
	HadDeadline bool
	Library     map[string]string
}

var (
	stubImageModels = []mediagen.Model{{ID: PickedImageModel, Name: "GPT Image 2"}, {ID: ImageChatModel, Name: "Gemini 3 Pro Image"}}
	stubAudioModels = []mediagen.Model{{ID: "google/lyria-3-pro-preview"}, {ID: "google/lyria-3-clip-preview"}, {ID: "openai/gpt-audio-mini"}}
)

func (s *StubImages) Check(_ context.Context, req mediagen.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if s.CatalogErr != nil {
		return s.CatalogErr
	}
	if err := mediagen.Supported(s.models(req.Kind), req.Model); req.Kind.UsesModel() && err != nil {
		return err
	}
	return s.CheckContent(req)
}

func (s *StubImages) CheckContent(req mediagen.Request) error {
	if s.CheckErr != nil {
		return s.CheckErr
	}
	if err := req.ValidateContent(); err != nil {
		return err
	}
	_, err := s.Sources(req)
	return err
}

func (s *StubImages) Generates(_ context.Context, kind mediagen.Kind, model string) (bool, error) {
	if s.CatalogErr != nil {
		return false, s.CatalogErr
	}
	return mediagen.Supported(s.models(kind), model) == nil, nil
}

func (s *StubImages) models(kind mediagen.Kind) []mediagen.Model {
	if kind == mediagen.KindImage {
		return stubImageModels
	}
	return stubAudioModels
}

func (s *StubImages) DefaultModel(_ context.Context, kind mediagen.Kind) (mediagen.Model, error) {
	if s.CatalogErr != nil {
		return mediagen.Model{}, s.CatalogErr
	}
	model, _ := mediagen.DefaultModel(s.models(kind))
	return model, nil
}

func (s *StubImages) Sources(req mediagen.Request) ([]mediagen.Source, error) {
	roles := req.SourceRoles()
	refs := make([]mediagen.Source, 0, len(roles))
	for _, role := range roles {
		url, ok := s.Library[role.MediaID]
		if !ok {
			return nil, &mediagen.ValidationError{Issues: []mediagen.FieldIssue{{Field: role.Field, Code: mediagen.CodeNotFound}}}
		}
		refs = append(refs, mediagen.Source{MediaID: role.MediaID, URL: url})
	}
	return refs, nil
}

func (s *StubImages) Request(_ context.Context, req mediagen.Request, requestedBy string) (*mediagen.Job, error) {
	s.Requested, s.RequestedBy = req, requestedBy
	if s.RequestErr != nil {
		return nil, s.RequestErr
	}
	return &mediagen.Job{ID: "job-1", WorkspaceID: req.WorkspaceID, Status: mediagen.StatusQueued, SourceMediaID: req.SourceMediaID, ReferenceMediaIDs: req.ReferenceMediaIDs}, nil
}

func (s *StubImages) Wait(ctx context.Context, _, id string) (*mediagen.Job, error) {
	s.WaitedFor = id
	_, s.HadDeadline = ctx.Deadline()
	return s.Final, s.WaitErr
}

const (
	PickedImageModel = "openai/gpt-image-2"
	ImageChatModel   = "google/gemini-3-pro-image"
)

var (
	ImageSession     = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1", Model: "anthropic/claude-sonnet-4"}
	ImageChatSession = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1", Model: ImageChatModel}
)

func ProposedImageArgs() map[string]interface{} {
	return map[string]interface{}{"prompt": "pizza artesanal", "aspect": "portrait"}
}

func ImageArgs() map[string]interface{} {
	args := ProposedImageArgs()
	args["image_model"] = PickedImageModel
	return args
}
