package workflow_usecase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/conversation"
	template_domain "vozko/domain/whatsapp/template"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workflow"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
	"vozko/usecases/workflow/node_executors"
)

type sandboxRun struct {
	outbound chan SimOutboundMessage
	effects  *simEffects
	registry *NodeExecutorRegistry
}

func newSandboxRun(deps ExecutorDeps) *sandboxRun {
	return newSandboxRunConfirming(deps, false)
}

func newSandboxRunConfirming(deps ExecutorDeps, realCalls bool) *sandboxRun {
	outbound := make(chan SimOutboundMessage, 32)
	effects := newSimEffects()
	if deps.MediaURLs == nil {
		deps.MediaURLs = externalMediaURLs()
	}
	registry := newSimulationRegistry(deps, SimSandbox{
		WorkspaceID: "ws1", LeadID: "lead-1", Messages: &simMessageRepo{}, Outbound: sendInto(outbound), Window: newSimMessageWindow("lead-1", true), Effects: effects,
		RealCallsConfirmed: func() bool { return realCalls },
	})
	return &sandboxRun{outbound: outbound, effects: effects, registry: registry}
}

func TestASandboxMediaSendDownloadsOnlyAfterRealCallsAreConfirmed(t *testing.T) {
	var hits atomic.Int32
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nfake-png-bytes"))
	}))
	defer media.Close()
	node := workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendMedia, Config: map[string]interface{}{"media_url": media.URL + "/fotos/boas-vindas.png", "caption": "Bem-vindo"}}

	unconfirmed := newSandboxRun(ExecutorDeps{MediaFetcher: loopbackMediaFetcher()})
	unconfirmed.execute(t, node)
	sent := unconfirmed.sent()
	if hits.Load() != 0 {
		t.Fatal("the sandbox downloaded the media without a real-call confirmation")
	}
	if len(sent) != 1 || sent[0].Message.Media == nil || sent[0].Message.Media.URL != media.URL+"/fotos/boas-vindas.png" || sent[0].Message.MediaType != conversation.MediaTypeImage {
		t.Fatalf("sent = %+v, the media must still be described by its link", sent)
	}

	confirmed := newSandboxRunConfirming(ExecutorDeps{MediaFetcher: loopbackMediaFetcher()}, true)
	confirmed.execute(t, node)
	if got := confirmed.sent(); hits.Load() != 1 || len(got) != 1 || got[0].Message.Media.MimeType != "image/png" {
		t.Fatalf("hits = %d sent = %+v, a confirmed test sends like production", hits.Load(), got)
	}
}

func loopbackMediaFetcher() node_executors.MediaFetcher {
	return node_executors.MediaFetcherOf(remotefile.NewDownloader(&http.Client{Timeout: 5 * time.Second}))
}

func TestAConfirmedSandboxDownloadsThroughTheProductionGuard(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\ninternal"))
	}))
	defer internal.Close()
	node := workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendMedia, Config: map[string]interface{}{"media_url": internal.URL + "/foto.png"}}

	guarded := node_executors.MediaFetcherOf(remotefile.NewDownloader(netguard.NewHTTPClient(2 * time.Second)))
	for name, deps := range map[string]ExecutorDeps{"guarded": {MediaFetcher: guarded}, "unwired": {}} {
		t.Run(name, func(t *testing.T) {
			box := newSandboxRunConfirming(deps, true)
			box.execute(t, node)
			if hits.Load() != 0 {
				t.Fatal("the sandbox reached an internal address")
			}
			if sent := box.sent(); len(sent) != 0 {
				t.Fatalf("sent = %+v, a refused download sends nothing", sent)
			}
		})
	}
}

func (s *sandboxRun) execute(t *testing.T, node workflow.Node) *workflow.NodeResult {
	t.Helper()
	g := workflow.Graph{Nodes: []workflow.Node{node, {ID: "end", Type: workflow.NodeTypeEnd}}, Edges: []workflow.Edge{{Source: node.ID, Target: "end"}}}
	run := &workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws1", EntryID: "sim-entry", EntryType: "whatsapp", State: workflow.NewRunState()}
	executor, ok := s.registry.Get(node.Type)
	if !ok {
		t.Fatalf("no executor for %s", node.Type)
	}
	ctx := &workflow.NodeContext{Run: run, Node: &g.Nodes[0], Graph: &g, Workflow: &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws1", Graph: g}, State: &run.State, Trace: &workflow.StepTrace{}}
	result, err := executor.Execute(ctx)
	if err != nil {
		t.Fatalf("execute %s: %v", node.Type, err)
	}
	return result
}

func (s *sandboxRun) sent() []SimOutboundMessage {
	var out []SimOutboundMessage
	for {
		select {
		case msg := <-s.outbound:
			out = append(out, msg)
		default:
			return out
		}
	}
}

func wireFields(t *testing.T, m *conversation.Message) map[string]interface{} {
	t.Helper()
	if m == nil {
		t.Fatal("the message_sent payload has no structured message")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type readyTemplates struct {
	template_domain.Repository
}

func (readyTemplates) FindByID(string) (*template_domain.Template, error) {
	return &template_domain.Template{
		ID: "t-1", Name: "boas_vindas", Language: "pt_BR", Status: template_domain.TemplateStatusApproved,
		Category: template_domain.TemplateCategoryMarketing,
		Components: []template_domain.TemplateComponent{
			{Type: "BODY", Text: "Oi {{1}}, tudo certo?"},
			{Type: "BUTTONS", Buttons: []template_domain.TemplateButton{{Type: "QUICK_REPLY", Text: "Sim"}}},
		},
	}, nil
}

const (
	firstSegment  = "Olá! Que bom ter você por aqui hoje."
	secondSegment = "Como posso ajudar com a sua matrícula?"
)

type twoParagraphModel struct {
	ai.Service
}

func (twoParagraphModel) Generate(context.Context, ai.GenerateInput) (*ai.GenerateOutput, error) {
	return &ai.GenerateOutput{Message: ai.Message{Role: "assistant", Content: firstSegment + "\n\n" + secondSegment}, FinishReason: "stop"}, nil
}

func TestEverySandboxSendReachesTheChatAsTheMessageTheConversationWouldStore(t *testing.T) {
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nfake-png-bytes"))
	}))
	defer media.Close()

	cases := []struct {
		name  string
		deps  ExecutorDeps
		node  workflow.Node
		check func(t *testing.T, sent []SimOutboundMessage)
	}{
		{
			name: "text",
			node: workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "Olá, tudo bem?"}},
			check: func(t *testing.T, sent []SimOutboundMessage) {
				m := wireFields(t, sent[0].Message)
				if sent[0].Text != "Olá, tudo bem?" || sent[0].Type != "text" || m["text"] != "Olá, tudo bem?" || m["messageType"] != "ai_response" || m["direction"] != "OUTBOUND" {
					t.Fatalf("sent = %+v, wire = %v", sent[0], m)
				}
				if m["id"] != sent[0].MessageID || m["deliveryStatus"] != "sent" {
					t.Fatalf("the message id and status must match the event: %v", m)
				}
			},
		},
		{
			name: "buttons",
			node: workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
				"body": "Quer continuar?", "buttons": []interface{}{map[string]interface{}{"id": "sim", "title": "Sim"}, map[string]interface{}{"id": "nao", "title": "Não"}},
			}},
			check: func(t *testing.T, sent []SimOutboundMessage) {
				var buttons conversation.SendButtonInput
				stored, ok := sent[0].Message.Buttons()
				if !ok {
					t.Fatalf("metadata = %s", sent[0].Message.Metadata)
				}
				buttons = stored
				if buttons.BodyText != "Quer continuar?" || len(buttons.Buttons) != 2 || buttons.Buttons[1] != (conversation.ButtonPayload{ID: "nao", Title: "Não"}) {
					t.Fatalf("buttons = %+v", buttons)
				}
				if !strings.Contains(sent[0].Text, "[buttons:") {
					t.Fatalf("the legacy text = %q", sent[0].Text)
				}
			},
		},
		{
			name: "list",
			node: workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
				"body": "Escolha o curso", "interactive_type": "list", "list_button": "Ver cursos",
				"sections": []interface{}{map[string]interface{}{"title": "Graduação", "rows": []interface{}{
					map[string]interface{}{"id": "dir", "title": "Direito", "description": "Noturno"},
				}}},
			}},
			check: func(t *testing.T, sent []SimOutboundMessage) {
				list, ok := sent[0].Message.List()
				if !ok || list.ButtonText != "Ver cursos" || list.Sections[0].Title != "Graduação" || list.Sections[0].Rows[0] != (conversation.ListRowPayload{ID: "dir", Title: "Direito", Description: "Noturno"}) {
					t.Fatalf("list = %+v, %v", list, ok)
				}
			},
		},
		{
			name: "uploaded image",
			node: workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendMedia, Config: map[string]interface{}{"media_url": media.URL + "/fotos/boas-vindas.png", "caption": "Bem-vindo"}},
			check: func(t *testing.T, sent []SimOutboundMessage) {
				m := sent[0].Message
				if m.MediaType != conversation.MediaTypeImage || m.Media == nil || m.Text != "Bem-vindo" || m.MessageType != conversation.MessageTypeMedia {
					t.Fatalf("message = %+v", m)
				}
				if m.Media.URL != media.URL+"/fotos/boas-vindas.png" || m.Media.MimeType != "image/png" || m.Media.Filename != "boas-vindas.png" || m.Media.Size != int64(len("\x89PNG\r\n\x1a\nfake-png-bytes")) {
					t.Fatalf("media = %+v", m.Media)
				}
				if sent[0].Text != "[image] Bem-vindo" {
					t.Fatalf("legacy text = %q", sent[0].Text)
				}
			},
		},
		{
			name: "template",
			deps: ExecutorDeps{TemplateRepo: readyTemplates{}},
			node: workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendTemplate, Config: map[string]interface{}{"template_id": "t-1", "params": map[string]interface{}{"1": "Ana"}}},
			check: func(t *testing.T, sent []SimOutboundMessage) {
				m := wireFields(t, sent[0].Message)
				meta, _ := m["metadata"].(map[string]interface{})
				if m["messageType"] != "template" || m["text"] != "Oi Ana, tudo certo?" || meta["template_name"] != "boas_vindas" || meta["language"] != "pt_BR" || meta["body_text"] != "Oi Ana, tudo certo?" {
					t.Fatalf("wire = %v", m)
				}
				if !strings.Contains(string(sent[0].Message.Metadata), `"Sim"`) {
					t.Fatalf("the template buttons are missing: %s", sent[0].Message.Metadata)
				}
			},
		},
		{
			name: "AI segmented reply",
			deps: ExecutorDeps{AIService: twoParagraphModel{}},
			node: workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAIAgent, Config: map[string]interface{}{
				"source": "prompt", "model": "stub/model", "instructions": "Responda", "response_mode": "segmented",
			}},
			check: func(t *testing.T, sent []SimOutboundMessage) {
				if len(sent) != 2 || sent[0].Message.Text != firstSegment || sent[1].Message.Text != secondSegment {
					t.Fatalf("segments = %+v", sent)
				}
				if sent[0].Message.MessageType != conversation.MessageTypeAIResponse {
					t.Fatalf("type = %s", sent[0].Message.MessageType)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := tc.deps
			deps.MediaFetcher = loopbackMediaFetcher()
			box := newSandboxRunConfirming(deps, true)
			box.execute(t, tc.node)
			sent := box.sent()
			if len(sent) == 0 {
				t.Fatal("nothing reached the simulated chat")
			}
			tc.check(t, sent)
			effects := box.effects.drain()
			if len(effects.Messages) != len(sent) {
				t.Fatalf("the step collected %d messages, the chat got %d", len(effects.Messages), len(sent))
			}
		})
	}
}

func TestTheSimulatedFormIsTheFormCardTheConversationShows(t *testing.T) {
	box := newSandboxRun(ExecutorDeps{})
	box.execute(t, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendForm, Config: map[string]interface{}{
		workflow.FormNodeFormIDKey: "form-1", "body": "Preencha a matrícula", "cta": "Abrir",
	}})
	sent := box.sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	var card struct {
		Form wf.MessageCard `json:"form"`
	}
	if err := json.Unmarshal(sent[0].Message.Metadata, &card); err != nil {
		t.Fatal(err)
	}
	if card.Form.Kind != wf.CardMessage || card.Form.FormID != "form-1" || card.Form.CTA != "Abrir" || card.Form.SendID != sent[0].MessageID {
		t.Fatalf("card = %+v", card.Form)
	}
	if sent[0].Type != simFormMessageType || sent[0].Message.Text != "Preencha a matrícula" {
		t.Fatalf("sent = %+v", sent[0])
	}
}

func TestASendTheSenderNeverRecordedStillReachesTheChat(t *testing.T) {
	outbound := make(chan SimOutboundMessage, 2)
	effects := newSimEffects()
	client := newSimWhatsAppClient(newSimChannel(sendInto(outbound), effects))

	out, err := client.SendTextMessage(context.Background(), conversation.SendTextMessageInput{To: simContactNumber, Body: "sem registro"})
	if err != nil {
		t.Fatal(err)
	}
	if len(outbound) != 0 {
		t.Fatal("a send waits for its record before reaching the chat")
	}

	drained := effects.drain()
	if len(outbound) != 1 || len(drained.Messages) != 1 {
		t.Fatalf("outbound %d, drained %d; the step end must flush an unrecorded send", len(outbound), len(drained.Messages))
	}
	msg := <-outbound
	if msg.MessageID != out.MessageID || msg.Message.Text != "sem registro" {
		t.Fatalf("message = %+v", msg)
	}
}

type everythingStoredHere struct{}

func (everythingStoredHere) OwnsURL(string) bool { return true }

func TestASandboxRefusesOurFileThatThisConversationCannotSend(t *testing.T) {
	node := workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendMedia, Config: map[string]interface{}{"media_url": "https://cdn.vozko.test/conversations/other/nota.png"}}

	box := newSandboxRunConfirming(ExecutorDeps{MediaFetcher: loopbackMediaFetcher(), MediaURLs: &conversation.MediaURLResolver{Storage: everythingStoredHere{}}}, true)
	box.execute(t, node)

	if sent := box.sent(); len(sent) != 0 {
		t.Fatalf("sent = %+v, a file no rule allows must not reach the chat", sent)
	}
}
