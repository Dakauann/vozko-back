package copilottools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/workspace"
)

type stubImages struct {
	catalogErr  error
	checkErr    error
	requestErr  error
	waitErr     error
	final       *mediagen.Job
	requested   mediagen.Request
	requestedBy string
	waitedFor   string
	hadDeadline bool
	library     map[string]string
}

var (
	stubImageModels = []mediagen.Model{{ID: pickedImageModel, Name: "GPT Image 2"}, {ID: imageChatModel, Name: "Gemini 3 Pro Image"}}
	stubAudioModels = []mediagen.Model{{ID: "google/lyria-3-pro-preview"}, {ID: "google/lyria-3-clip-preview"}, {ID: "openai/gpt-audio-mini"}}
)

func (s *stubImages) Check(_ context.Context, req mediagen.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if s.catalogErr != nil {
		return s.catalogErr
	}
	if err := mediagen.Supported(s.models(req.Kind), req.Model); req.Kind.UsesModel() && err != nil {
		return err
	}
	return s.CheckContent(req)
}

func (s *stubImages) CheckContent(req mediagen.Request) error {
	if s.checkErr != nil {
		return s.checkErr
	}
	if err := req.ValidateContent(); err != nil {
		return err
	}
	_, err := s.Sources(req)
	return err
}

func (s *stubImages) Generates(_ context.Context, kind mediagen.Kind, model string) (bool, error) {
	if s.catalogErr != nil {
		return false, s.catalogErr
	}
	return mediagen.Supported(s.models(kind), model) == nil, nil
}

func (s *stubImages) models(kind mediagen.Kind) []mediagen.Model {
	if kind == mediagen.KindImage {
		return stubImageModels
	}
	return stubAudioModels
}

func (s *stubImages) DefaultModel(_ context.Context, kind mediagen.Kind) (mediagen.Model, error) {
	if s.catalogErr != nil {
		return mediagen.Model{}, s.catalogErr
	}
	model, _ := mediagen.DefaultModel(kind, s.models(kind))
	return model, nil
}

func (s *stubImages) Sources(req mediagen.Request) ([]mediagen.Source, error) {
	roles := req.SourceRoles()
	refs := make([]mediagen.Source, 0, len(roles))
	for _, role := range roles {
		url, ok := s.library[role.MediaID]
		if !ok {
			return nil, &mediagen.ValidationError{Issues: []mediagen.FieldIssue{{Field: role.Field, Code: mediagen.CodeNotFound}}}
		}
		refs = append(refs, mediagen.Source{MediaID: role.MediaID, URL: url})
	}
	return refs, nil
}

func (s *stubImages) Request(_ context.Context, req mediagen.Request, requestedBy string) (*mediagen.Job, error) {
	s.requested, s.requestedBy = req, requestedBy
	if s.requestErr != nil {
		return nil, s.requestErr
	}
	return &mediagen.Job{ID: "job-1", WorkspaceID: req.WorkspaceID, Status: mediagen.StatusQueued}, nil
}

func (s *stubImages) Wait(ctx context.Context, _, id string) (*mediagen.Job, error) {
	s.waitedFor = id
	_, s.hadDeadline = ctx.Deadline()
	return s.final, s.waitErr
}

const (
	pickedImageModel = "openai/gpt-image-2"
	imageChatModel   = "google/gemini-3-pro-image"
)

var (
	imageSession     = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1", Model: "anthropic/claude-sonnet-4"}
	imageChatSession = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1", Model: imageChatModel}
)

func proposedImageArgs() map[string]interface{} {
	return map[string]interface{}{"prompt": "pizza artesanal", "aspect": "portrait"}
}

func imageArgs() map[string]interface{} {
	args := proposedImageArgs()
	args["image_model"] = pickedImageModel
	return args
}

func TestGenerateImageIsAnApprovedMediaCreation(t *testing.T) {
	tool := NewGenerateImageTool(&stubImages{})
	m := tool.Meta()
	if !m.Mutating || m.Resource != workspace.ResourceMedia || m.Action != workspace.ActionCreate || tool.Definition().Name != "generate_image" {
		t.Fatalf("meta %+v name %s", m, tool.Definition().Name)
	}
	if _, ok := tool.(copilot.Describer); !ok {
		t.Fatal("the approval card cannot show the image")
	}
}

func TestGenerateImageChecksTheRequestAndFundsBeforeApproval(t *testing.T) {
	tool := NewGenerateImageTool(&stubImages{}).(copilot.Validator)
	if err := tool.Validate(context.Background(), imageSession, imageArgs()); err != nil {
		t.Fatal(err)
	}
	if err := tool.Validate(context.Background(), imageSession, map[string]interface{}{"prompt": "x", "aspect": "wide"}); err == nil {
		t.Fatal("unknown aspect accepted")
	}
	broke := NewGenerateImageTool(&stubImages{checkErr: errors.New("saldo insuficiente")}).(copilot.Validator)
	if err := broke.Validate(context.Background(), imageSession, imageArgs()); err == nil {
		t.Fatal("no funds accepted")
	}
}

func TestGenerateImageCardNamesThePromptFormatAndCost(t *testing.T) {
	fields := NewGenerateImageTool(&stubImages{}).(copilot.Describer).Describe(context.Background(), imageSession, imageArgs())
	text := ""
	for _, f := range fields {
		text += f.Key + "=" + f.Value + ";"
	}
	for _, want := range []string{"pizza artesanal", "portrait", "cobrado do saldo pelo custo do provedor"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card %s misses %q", text, want)
		}
	}
}

func TestGenerateImageWaitsForTheQueuedJob(t *testing.T) {
	images := &stubImages{final: &mediagen.Job{ID: "job-1", Kind: mediagen.KindImage, Status: mediagen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg", Model: "model-x"}}
	res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, imageArgs())
	if res.Status != copilot.StatusOK {
		t.Fatalf("result %+v", res)
	}
	data := res.Data.(map[string]interface{})
	if data["media_id"] != "m-1" || data["media_url"] != "https://cdn/x.jpg" || data["model"] != "model-x" {
		t.Fatalf("data %v", data)
	}
	if !reflect.DeepEqual(images.requested, mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws-1", Model: pickedImageModel, Prompt: "pizza artesanal", Aspect: mediagen.AspectPortrait}) || images.requestedBy != "u-1" {
		t.Fatalf("requested %+v by %q", images.requested, images.requestedBy)
	}
	if images.waitedFor != "job-1" || !images.hadDeadline {
		t.Fatalf("waited for %q deadline %v", images.waitedFor, images.hadDeadline)
	}
}

func TestGenerateImageExplainsEveryFailure(t *testing.T) {
	codes := []mediagen.FailureCode{mediagen.FailureGeneration, mediagen.FailureStorage, mediagen.FailureTimedOut, mediagen.FailureEnqueue, mediagen.FailureInsufficientFunds, mediagen.FailureReferenceUnavailable}
	seen := map[string]bool{}
	for _, code := range codes {
		images := &stubImages{final: &mediagen.Job{ID: "job-1", Status: mediagen.StatusFailed, FailureCode: code}}
		res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, imageArgs())
		if res.Status != copilot.StatusError || res.Message == "" {
			t.Fatalf("%s: %+v", code, res)
		}
		seen[res.Message] = true
	}
	if len(seen) != len(codes) {
		t.Fatalf("failures share messages: %v", seen)
	}
}

func TestGenerateImageThatOutlivesTheWaitSaysItIsStillRunning(t *testing.T) {
	images := &stubImages{final: &mediagen.Job{ID: "job-1", Status: mediagen.StatusRunning}, waitErr: context.DeadlineExceeded}
	res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, imageArgs())
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "mesmos dados") {
		t.Fatalf("result %+v", res)
	}
}

func TestGenerateImageRefusedRequestNeverWaits(t *testing.T) {
	images := &stubImages{requestErr: errors.New("rabbit down")}
	res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, imageArgs())
	if res.Status != copilot.StatusError || images.waitedFor != "" {
		t.Fatalf("result %+v waited %q", res, images.waitedFor)
	}
}

const (
	firstReference  = "5b0c6f1e-8d7a-4c2b-9e3f-1a2b3c4d5e6f"
	secondReference = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
)

func referencedImageArgs(ids ...interface{}) map[string]interface{} {
	args := imageArgs()
	args["reference_media_ids"] = ids
	return args
}

func referenceLibrary() *stubImages {
	return &stubImages{library: map[string]string{firstReference: "https://cdn/first.png", secondReference: "https://cdn/second.jpg"}}
}

func TestGenerateImageOffersReferenceImages(t *testing.T) {
	param, ok := NewGenerateImageTool(&stubImages{}).Definition().Parameters["reference_media_ids"]
	if !ok || param.Type != "array" || param.Items == nil || param.Items.Type != "string" || !strings.Contains(param.Description, "anex") {
		t.Fatalf("reference parameter %+v", param)
	}
}

func TestGenerateImageSendsTheReferencesInOrder(t *testing.T) {
	images := referenceLibrary()
	images.final = &mediagen.Job{ID: "job-1", Status: mediagen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg"}
	res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, referencedImageArgs(secondReference, firstReference))
	if res.Status != copilot.StatusOK || !reflect.DeepEqual(images.requested.ReferenceMediaIDs, []string{secondReference, firstReference}) {
		t.Fatalf("result %+v requested %+v", res, images.requested)
	}
}

func TestGenerateImageRefusesReferencesItCannotUse(t *testing.T) {
	tool := NewGenerateImageTool(referenceLibrary()).(copilot.Validator)
	if err := tool.Validate(context.Background(), imageSession, referencedImageArgs(firstReference)); err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]interface{}{{"invented"}, {"0f0f0f0f-0000-4000-8000-000000000000"}, {firstReference, firstReference}} {
		if err := tool.Validate(context.Background(), imageSession, referencedImageArgs(refs...)); err == nil {
			t.Fatalf("%v accepted", refs)
		}
	}
}

func TestGenerateImageCardShowsTheReferenceThumbnails(t *testing.T) {
	tool := NewGenerateImageTool(referenceLibrary()).(copilot.Previewer)
	preview := tool.Preview(context.Background(), imageSession, referencedImageArgs(firstReference, secondReference))
	if preview == nil || preview.Kind != PreviewImageReferences {
		t.Fatalf("preview %+v", preview)
	}
	want := ImageReferencesPreview{References: []ImageReferencePreview{{MediaID: firstReference, URL: "https://cdn/first.png"}, {MediaID: secondReference, URL: "https://cdn/second.jpg"}}}
	if !reflect.DeepEqual(preview.Data, want) {
		t.Fatalf("data %+v", preview.Data)
	}
	if tool.Preview(context.Background(), imageSession, imageArgs()) != nil {
		t.Fatal("a request without references has nothing to preview")
	}
}

func TestATextOnlyChatModelAsksForAnImageModelOnTheCard(t *testing.T) {
	choices, err := NewGenerateImageTool(&stubImages{}).(copilot.ChoiceAsker).Choices(context.Background(), imageSession, proposedImageArgs())
	if err != nil || len(choices) != 1 || choices[0].Key != "image_model" || choices[0].Kind != copilot.ChoiceImageModel {
		t.Fatalf("choices %+v err %v", choices, err)
	}
	if err := NewGenerateImageTool(&stubImages{}).(copilot.Validator).Validate(context.Background(), imageSession, proposedImageArgs()); err != nil {
		t.Fatalf("a proposal before the choice was refused: %v", err)
	}
}

func TestAnImageCapableChatModelGeneratesTheImageItself(t *testing.T) {
	images := &stubImages{final: &mediagen.Job{ID: "job-1", Status: mediagen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg"}}
	tool := NewGenerateImageTool(images)
	choices, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), imageChatSession, proposedImageArgs())
	if err != nil || len(choices) != 0 {
		t.Fatalf("choices %+v err %v", choices, err)
	}
	if res := tool.Execute(context.Background(), imageChatSession, proposedImageArgs()); res.Status != copilot.StatusOK || images.requested.Model != imageChatModel {
		t.Fatalf("result %+v requested %+v", res, images.requested)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), imageChatSession, proposedImageArgs())
	shown := false
	for _, f := range fields {
		shown = shown || (f.Key == "image_model" && f.Value == imageChatModel)
	}
	if !shown {
		t.Fatalf("the card does not name the model: %+v", fields)
	}
}

func TestNoImageIsRequestedWithoutAModel(t *testing.T) {
	images := &stubImages{}
	res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, proposedImageArgs())
	if res.Status != copilot.StatusError || images.requested.Prompt != "" {
		t.Fatalf("result %+v requested %+v", res, images.requested)
	}
}

func TestAChosenModelOutsideTheCatalogIsRefused(t *testing.T) {
	args := imageArgs()
	args["image_model"] = "anthropic/claude-sonnet-4"
	if err := NewGenerateImageTool(&stubImages{}).(copilot.Validator).Validate(context.Background(), imageSession, args); err == nil {
		t.Fatal("a model outside the image catalog was accepted")
	}
}

func TestAnUnavailableCatalogStopsTheProposal(t *testing.T) {
	tool := NewGenerateImageTool(&stubImages{catalogErr: errors.New("catalog down")})
	if _, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), imageSession, proposedImageArgs()); err == nil {
		t.Fatal("choices were offered without the catalog")
	}
	if err := tool.(copilot.Validator).Validate(context.Background(), imageSession, imageArgs()); err == nil {
		t.Fatal("a proposal passed without the catalog")
	}
}
