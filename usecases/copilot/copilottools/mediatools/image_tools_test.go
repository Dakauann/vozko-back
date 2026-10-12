package mediatools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func TestGenerateImageIsAnApprovedMediaCreation(t *testing.T) {
	tool := NewGenerateImageTool(&tooltest.StubImages{})
	m := tool.Meta()
	if !m.Mutating || m.Resource != workspace.ResourceMedia || m.Action != workspace.ActionCreate || tool.Definition().Name != "generate_image" {
		t.Fatalf("meta %+v name %s", m, tool.Definition().Name)
	}
	if _, ok := tool.(copilot.Describer); !ok {
		t.Fatal("the approval card cannot show the image")
	}
}

func TestGenerateImageChecksTheRequestAndFundsBeforeApproval(t *testing.T) {
	tool := NewGenerateImageTool(&tooltest.StubImages{}).(copilot.Validator)
	if err := tool.Validate(context.Background(), tooltest.ImageSession, tooltest.ImageArgs()); err != nil {
		t.Fatal(err)
	}
	if err := tool.Validate(context.Background(), tooltest.ImageSession, map[string]interface{}{"prompt": "x", "aspect": "wide"}); err == nil {
		t.Fatal("unknown aspect accepted")
	}
	broke := NewGenerateImageTool(&tooltest.StubImages{CheckErr: errors.New("saldo insuficiente")}).(copilot.Validator)
	if err := broke.Validate(context.Background(), tooltest.ImageSession, tooltest.ImageArgs()); err == nil {
		t.Fatal("no funds accepted")
	}
}

func TestGenerateImageCardNamesThePromptFormatAndCost(t *testing.T) {
	fields := NewGenerateImageTool(&tooltest.StubImages{}).(copilot.Describer).Describe(context.Background(), tooltest.ImageSession, tooltest.ImageArgs())
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
	images := &tooltest.StubImages{Final: &mediagen.Job{ID: "job-1", Kind: mediagen.KindImage, Status: mediagen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg", Model: "model-x"}}
	res := NewGenerateImageTool(images).Execute(context.Background(), tooltest.ImageSession, tooltest.ImageArgs())
	if res.Status != copilot.StatusOK {
		t.Fatalf("result %+v", res)
	}
	data := res.Data.(map[string]interface{})
	if data["media_id"] != "m-1" || data["media_url"] != "https://cdn/x.jpg" || data["model"] != "model-x" {
		t.Fatalf("data %v", data)
	}
	if !reflect.DeepEqual(images.Requested, mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws-1", Model: tooltest.PickedImageModel, Prompt: "pizza artesanal", Aspect: mediagen.AspectPortrait}) || images.RequestedBy != "u-1" {
		t.Fatalf("requested %+v by %q", images.Requested, images.RequestedBy)
	}
	if images.WaitedFor != "job-1" || !images.HadDeadline {
		t.Fatalf("waited for %q deadline %v", images.WaitedFor, images.HadDeadline)
	}
}

func TestGenerateImageExplainsEveryFailure(t *testing.T) {
	codes := []mediagen.FailureCode{mediagen.FailureGeneration, mediagen.FailureStorage, mediagen.FailureTimedOut, mediagen.FailureEnqueue, mediagen.FailureInsufficientFunds, mediagen.FailureReferenceUnavailable}
	seen := map[string]bool{}
	for _, code := range codes {
		images := &tooltest.StubImages{Final: &mediagen.Job{ID: "job-1", Status: mediagen.StatusFailed, FailureCode: code}}
		res := NewGenerateImageTool(images).Execute(context.Background(), tooltest.ImageSession, tooltest.ImageArgs())
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
	images := &tooltest.StubImages{Final: &mediagen.Job{ID: "job-1", Status: mediagen.StatusRunning}, WaitErr: context.DeadlineExceeded}
	res := NewGenerateImageTool(images).Execute(context.Background(), tooltest.ImageSession, tooltest.ImageArgs())
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "mesmos dados") {
		t.Fatalf("result %+v", res)
	}
}

func TestGenerateImageRefusedRequestNeverWaits(t *testing.T) {
	images := &tooltest.StubImages{RequestErr: errors.New("rabbit down")}
	res := NewGenerateImageTool(images).Execute(context.Background(), tooltest.ImageSession, tooltest.ImageArgs())
	if res.Status != copilot.StatusError || images.WaitedFor != "" {
		t.Fatalf("result %+v waited %q", res, images.WaitedFor)
	}
}

const (
	firstReference  = "5b0c6f1e-8d7a-4c2b-9e3f-1a2b3c4d5e6f"
	secondReference = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
)

func referencedImageArgs(ids ...interface{}) map[string]interface{} {
	args := tooltest.ImageArgs()
	args["reference_media_ids"] = ids
	return args
}

func referenceLibrary() *tooltest.StubImages {
	return &tooltest.StubImages{Library: map[string]string{firstReference: "https://cdn/first.png", secondReference: "https://cdn/second.jpg"}}
}

func TestGenerateImageOffersReferenceImages(t *testing.T) {
	param, ok := NewGenerateImageTool(&tooltest.StubImages{}).Definition().Parameters["reference_media_ids"]
	if !ok || param.Type != "array" || param.Items == nil || param.Items.Type != "string" || !strings.Contains(param.Description, "anex") {
		t.Fatalf("reference parameter %+v", param)
	}
}

func TestGenerateImageSendsTheReferencesInOrder(t *testing.T) {
	images := referenceLibrary()
	images.Final = &mediagen.Job{ID: "job-1", Status: mediagen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg"}
	res := NewGenerateImageTool(images).Execute(context.Background(), tooltest.ImageSession, referencedImageArgs(secondReference, firstReference))
	if res.Status != copilot.StatusOK || !reflect.DeepEqual(images.Requested.ReferenceMediaIDs, []string{secondReference, firstReference}) {
		t.Fatalf("result %+v requested %+v", res, images.Requested)
	}
}

func TestGenerateImageRefusesReferencesItCannotUse(t *testing.T) {
	tool := NewGenerateImageTool(referenceLibrary()).(copilot.Validator)
	if err := tool.Validate(context.Background(), tooltest.ImageSession, referencedImageArgs(firstReference)); err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]interface{}{{"invented"}, {"0f0f0f0f-0000-4000-8000-000000000000"}, {firstReference, firstReference}} {
		if err := tool.Validate(context.Background(), tooltest.ImageSession, referencedImageArgs(refs...)); err == nil {
			t.Fatalf("%v accepted", refs)
		}
	}
}

func TestGenerateImageCardShowsTheReferenceThumbnails(t *testing.T) {
	tool := NewGenerateImageTool(referenceLibrary()).(copilot.Previewer)
	preview := tool.Preview(context.Background(), tooltest.ImageSession, referencedImageArgs(firstReference, secondReference))
	if preview == nil || preview.Kind != PreviewImageReferences {
		t.Fatalf("preview %+v", preview)
	}
	want := ImageReferencesPreview{References: []ImageReferencePreview{{MediaID: firstReference, URL: "https://cdn/first.png"}, {MediaID: secondReference, URL: "https://cdn/second.jpg"}}}
	if !reflect.DeepEqual(preview.Data, want) {
		t.Fatalf("data %+v", preview.Data)
	}
	if tool.Preview(context.Background(), tooltest.ImageSession, tooltest.ImageArgs()) != nil {
		t.Fatal("a request without references has nothing to preview")
	}
}

func TestATextOnlyChatModelAsksForAnImageModelOnTheCard(t *testing.T) {
	choices, err := NewGenerateImageTool(&tooltest.StubImages{}).(copilot.ChoiceAsker).Choices(context.Background(), tooltest.ImageSession, tooltest.ProposedImageArgs())
	if err != nil || len(choices) != 1 || choices[0].Key != "image_model" || choices[0].Kind != copilot.ChoiceImageModel {
		t.Fatalf("choices %+v err %v", choices, err)
	}
	if err := NewGenerateImageTool(&tooltest.StubImages{}).(copilot.Validator).Validate(context.Background(), tooltest.ImageSession, tooltest.ProposedImageArgs()); err != nil {
		t.Fatalf("a proposal before the choice was refused: %v", err)
	}
}

func TestAnImageCapableChatModelGeneratesTheImageItself(t *testing.T) {
	images := &tooltest.StubImages{Final: &mediagen.Job{ID: "job-1", Status: mediagen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg"}}
	tool := NewGenerateImageTool(images)
	choices, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), tooltest.ImageChatSession, tooltest.ProposedImageArgs())
	if err != nil || len(choices) != 0 {
		t.Fatalf("choices %+v err %v", choices, err)
	}
	if res := tool.Execute(context.Background(), tooltest.ImageChatSession, tooltest.ProposedImageArgs()); res.Status != copilot.StatusOK || images.Requested.Model != tooltest.ImageChatModel {
		t.Fatalf("result %+v requested %+v", res, images.Requested)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), tooltest.ImageChatSession, tooltest.ProposedImageArgs())
	shown := false
	for _, f := range fields {
		shown = shown || (f.Key == "image_model" && f.Value == tooltest.ImageChatModel)
	}
	if !shown {
		t.Fatalf("the card does not name the model: %+v", fields)
	}
}

func TestNoImageIsRequestedWithoutAModel(t *testing.T) {
	images := &tooltest.StubImages{}
	res := NewGenerateImageTool(images).Execute(context.Background(), tooltest.ImageSession, tooltest.ProposedImageArgs())
	if res.Status != copilot.StatusError || images.Requested.Prompt != "" {
		t.Fatalf("result %+v requested %+v", res, images.Requested)
	}
}

func TestAChosenModelOutsideTheCatalogIsRefused(t *testing.T) {
	args := tooltest.ImageArgs()
	args["image_model"] = "anthropic/claude-sonnet-4"
	if err := NewGenerateImageTool(&tooltest.StubImages{}).(copilot.Validator).Validate(context.Background(), tooltest.ImageSession, args); err == nil {
		t.Fatal("a model outside the image catalog was accepted")
	}
}

func TestAnUnavailableCatalogStopsTheProposal(t *testing.T) {
	tool := NewGenerateImageTool(&tooltest.StubImages{CatalogErr: errors.New("catalog down")})
	if _, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), tooltest.ImageSession, tooltest.ProposedImageArgs()); err == nil {
		t.Fatal("choices were offered without the catalog")
	}
	if err := tool.(copilot.Validator).Validate(context.Background(), tooltest.ImageSession, tooltest.ImageArgs()); err == nil {
		t.Fatal("a proposal passed without the catalog")
	}
}
