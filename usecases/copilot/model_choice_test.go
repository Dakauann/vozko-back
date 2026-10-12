package copilot_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

func catalogModel(id string, caps []ai.ModelCapability, reasoning *ai.ReasoningOptions) ai.ModelInfo {
	return ai.ModelInfo{ID: id, Capabilities: caps, Reasoning: reasoning}
}

var thinkerLevels = &ai.ReasoningOptions{Levels: []ai.ReasoningLevel{ai.ReasoningLow, ai.ReasoningMedium, ai.ReasoningHigh}, Default: ai.ReasoningMedium}

type failures struct{ codes []string }

func (f *failures) emit(kind string, payload interface{}) {
	if kind != "error" {
		return
	}
	if body, ok := payload.(map[string]interface{}); ok {
		code, _ := body["code"].(string)
		f.codes = append(f.codes, code)
	}
}

func (f *failures) code() string {
	if len(f.codes) == 0 {
		return ""
	}
	return f.codes[0]
}

func TestTurnRefusesAModelWithoutToolUse(t *testing.T) {
	cases := []struct {
		name   string
		models []ai.ModelInfo
	}{
		{name: "the catalog says the model has no tools", models: []ai.ModelInfo{catalogModel("m", []ai.ModelCapability{ai.CapabilityVision}, nil)}},
		{name: "the model is not in the catalog", models: []ai.ModelInfo{catalogModel("other", []ai.ModelCapability{ai.CapabilityTools}, nil)}},
		{name: "the catalog is empty", models: []ai.ModelInfo{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
			prov := &scriptAI{texts: []string{"oi"}, models: tc.models}
			cp := &failures{}
			if err := newService(prov, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "oi"}, ownerCtx, cp.emit); err != nil {
				t.Fatalf("stream: %v", err)
			}
			if len(prov.inputs) != 0 {
				t.Fatal("the model was called although it cannot use tools")
			}
			if got := cp.code(); got != ai.RefusalModelWithoutTools {
				t.Fatalf("error code = %q, want %q", got, ai.RefusalModelWithoutTools)
			}
			if len(ms.created) != 0 {
				t.Fatalf("a refused turn stored %d messages", len(ms.created))
			}
		})
	}
}

func TestTurnRefusesHonestlyWhenTheCatalogCannotBeRead(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	prov := &scriptAI{texts: []string{"oi"}, catalogErr: ai.ErrProviderUnavailable}
	cp := &failures{}
	if err := newService(prov, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "oi"}, ownerCtx, cp.emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(prov.inputs) != 0 {
		t.Fatal("the model was called although its tools could not be confirmed")
	}
	if got := cp.code(); got != ai.RefusalModelCatalogUnavailable {
		t.Fatalf("error code = %q, want %q", got, ai.RefusalModelCatalogUnavailable)
	}
	if len(ms.created) != 0 {
		t.Fatalf("a refused turn stored %d messages", len(ms.created))
	}
}

func TestTurnRefusesAReasoningLevelTheModelDoesNotOffer(t *testing.T) {
	thread := testThread()
	thread.ReasoningLevel = "max"
	th, ms := &fakeThreads{thread: thread}, &fakeMessages{}
	prov := &scriptAI{texts: []string{"oi"}, models: []ai.ModelInfo{catalogModel("m", []ai.ModelCapability{ai.CapabilityTools, ai.CapabilityReasoning}, thinkerLevels)}}
	cp := &failures{}
	if err := newService(prov, th, ms).Stream(context.Background(), thread, copilot.UserMessage{Content: "oi"}, ownerCtx, cp.emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(prov.inputs) != 0 {
		t.Fatal("an unsupported level reached the model")
	}
	if got := cp.code(); got != ai.RefusalReasoningUnsupported {
		t.Fatalf("error code = %q, want %q", got, ai.RefusalReasoningUnsupported)
	}
}

func TestTurnSendsTheChosenReasoningLevelAndRemembersIt(t *testing.T) {
	thread := testThread()
	thread.ReasoningLevel = "high"
	th, ms := &fakeThreads{thread: thread}, &fakeMessages{}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"pronto"}, models: []ai.ModelInfo{catalogModel("m", []ai.ModelCapability{ai.CapabilityTools, ai.CapabilityReasoning}, thinkerLevels)}}
	if err := newService(prov, th, ms).Stream(context.Background(), thread, copilot.UserMessage{Content: "oi"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(prov.inputs) == 0 || prov.inputs[0].ReasoningEffort != ai.ReasoningHigh {
		t.Fatalf("inputs = %+v, want effort high", prov.inputs)
	}
	if th.touchedModel != "m" || th.touchedLevel != "high" {
		t.Fatalf("touched %q / %q, want m / high", th.touchedModel, th.touchedLevel)
	}
}

func TestTurnWithoutALevelLeavesTheModelDefault(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"pronto"}, models: []ai.ModelInfo{catalogModel("m", []ai.ModelCapability{ai.CapabilityTools, ai.CapabilityReasoning}, thinkerLevels)}}
	if err := newService(prov, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "oi"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if prov.inputs[0].ReasoningEffort != "" {
		t.Fatalf("effort = %q, want the model default", prov.inputs[0].ReasoningEffort)
	}
}

func TestABlindModelIsToldItCannotSeeAnAttachedImage(t *testing.T) {
	files := workspaceFiles{"img": {ID: "img", WorkspaceID: "ws1", URL: "https://files.test/ws1/logo.png", Type: "image"}}
	cases := []struct {
		name      string
		caps      []ai.ModelCapability
		wantBlind bool
	}{
		{name: "without vision", caps: []ai.ModelCapability{ai.CapabilityTools}, wantBlind: true},
		{name: "with vision", caps: []ai.ModelCapability{ai.CapabilityTools, ai.CapabilityVision}, wantBlind: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
			prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"ok"}, models: []ai.ModelInfo{catalogModel("m", tc.caps, nil)}}
			svc := NewService(agentloop.Engine{AI: prov}, NewRegistry(), &fakeAccess{}, openFunds{}, th, ms, files, nil, func() string { return "a" })
			cc := ownerCtx
			cc.WorkspaceID = "ws1"
			if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "o que tem aqui?", AttachmentIDs: []string{"img"}}, cc, (&capture{}).emit); err != nil {
				t.Fatal(err)
			}
			blind := false
			for _, m := range prov.inputs[0].Messages {
				if m.Role == ai.RoleUser && strings.Contains(m.Content, copilot.BlindImagesNote) {
					blind = true
				}
			}
			if blind != tc.wantBlind {
				t.Fatalf("blind note present = %v, want %v", blind, tc.wantBlind)
			}
		})
	}
}

func TestHistoryRebuildTellsABlindModelAboutEarlierImages(t *testing.T) {
	earlier := &aichat.Message{ThreadID: "t1", Role: aichat.RoleUser, Content: "veja", Attachments: []byte(`[{"mediaId":"img","name":"logo.png","kind":"image","url":"https://files.test/logo.png"}]`)}
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{list: []*aichat.Message{earlier}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"ok"}, models: []ai.ModelInfo{catalogModel("m", []ai.ModelCapability{ai.CapabilityTools}, nil)}}
	if err := newService(prov, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "e agora?"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prov.inputs[0].Messages[0].Content, copilot.BlindImagesNote) {
		t.Fatalf("history message = %q, want the blind note", prov.inputs[0].Messages[0].Content)
	}
}
