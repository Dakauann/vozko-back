package copilottools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/imagegen"
	"vozko/domain/workspace"
)

type stubImages struct {
	checkErr    error
	requestErr  error
	waitErr     error
	final       *imagegen.Job
	requested   imagegen.Request
	requestedBy string
	waitedFor   string
	hadDeadline bool
	library     map[string]string
}

func (s *stubImages) Check(req imagegen.Request) error {
	if s.checkErr != nil {
		return s.checkErr
	}
	if err := req.Validate(); err != nil {
		return err
	}
	_, err := s.References(req.WorkspaceID, req.ReferenceMediaIDs)
	return err
}

func (s *stubImages) References(_ string, ids []string) ([]imagegen.ReferenceImage, error) {
	refs := make([]imagegen.ReferenceImage, 0, len(ids))
	for _, id := range ids {
		url, ok := s.library[id]
		if !ok {
			return nil, &imagegen.ValidationError{Issues: []imagegen.FieldIssue{{Field: imagegen.FieldReferences, Code: imagegen.CodeNotFound}}}
		}
		refs = append(refs, imagegen.ReferenceImage{MediaID: id, URL: url})
	}
	return refs, nil
}

func (s *stubImages) Request(_ context.Context, req imagegen.Request, requestedBy string) (*imagegen.Job, error) {
	s.requested, s.requestedBy = req, requestedBy
	if s.requestErr != nil {
		return nil, s.requestErr
	}
	return &imagegen.Job{ID: "job-1", WorkspaceID: req.WorkspaceID, Status: imagegen.StatusQueued}, nil
}

func (s *stubImages) Wait(ctx context.Context, _, id string) (*imagegen.Job, error) {
	s.waitedFor = id
	_, s.hadDeadline = ctx.Deadline()
	return s.final, s.waitErr
}

var imageSession = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1"}

func imageArgs() map[string]interface{} {
	return map[string]interface{}{"prompt": "pizza artesanal", "aspect": "portrait"}
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
	for _, want := range []string{"pizza artesanal", "portrait", "cobrado do saldo como uso de IA"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card %s misses %q", text, want)
		}
	}
}

func TestGenerateImageWaitsForTheQueuedJob(t *testing.T) {
	images := &stubImages{final: &imagegen.Job{ID: "job-1", Status: imagegen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg", Model: "model-x"}}
	res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, imageArgs())
	if res.Status != copilot.StatusOK {
		t.Fatalf("result %+v", res)
	}
	data := res.Data.(map[string]interface{})
	if data["media_id"] != "m-1" || data["media_url"] != "https://cdn/x.jpg" || data["model"] != "model-x" {
		t.Fatalf("data %v", data)
	}
	if !reflect.DeepEqual(images.requested, imagegen.Request{WorkspaceID: "ws-1", Prompt: "pizza artesanal", Aspect: imagegen.AspectPortrait}) || images.requestedBy != "u-1" {
		t.Fatalf("requested %+v by %q", images.requested, images.requestedBy)
	}
	if images.waitedFor != "job-1" || !images.hadDeadline {
		t.Fatalf("waited for %q deadline %v", images.waitedFor, images.hadDeadline)
	}
}

func TestGenerateImageExplainsEveryFailure(t *testing.T) {
	codes := []imagegen.FailureCode{imagegen.FailureGeneration, imagegen.FailureStorage, imagegen.FailureTimedOut, imagegen.FailureEnqueue, imagegen.FailureInsufficientFunds, imagegen.FailureReferenceUnavailable}
	seen := map[string]bool{}
	for _, code := range codes {
		images := &stubImages{final: &imagegen.Job{ID: "job-1", Status: imagegen.StatusFailed, FailureCode: code}}
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
	images := &stubImages{final: &imagegen.Job{ID: "job-1", Status: imagegen.StatusRunning}, waitErr: context.DeadlineExceeded}
	res := NewGenerateImageTool(images).Execute(context.Background(), imageSession, imageArgs())
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "mesma descrição") {
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
	images.final = &imagegen.Job{ID: "job-1", Status: imagegen.StatusDone, MediaID: "m-1", MediaURL: "https://cdn/x.jpg"}
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
