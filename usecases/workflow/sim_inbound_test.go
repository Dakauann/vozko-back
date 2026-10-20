package workflow_usecase

import (
	"encoding/json"
	"reflect"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

func clientMsg(t *testing.T, kind string, data interface{}) simClientMsg {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return simClientMsg{Type: kind, Data: raw}
}

func TestEveryTesterReplyCarriesTheKeysTheWebhookWouldDeliver(t *testing.T) {
	cases := []struct {
		name string
		msg  func(t *testing.T) simClientMsg
		want map[string]interface{}
	}{
		{"typed text", func(t *testing.T) simClientMsg { return clientMsg(t, "reply", map[string]string{"text": "oi"}) },
			map[string]interface{}{"message": "oi", "message_type": "text"}},
		{"a tapped button", func(t *testing.T) simClientMsg {
			return clientMsg(t, "reply_selection", map[string]string{"kind": "button", "id": "sim", "title": "Sim"})
		}, map[string]interface{}{"message": "Sim", "message_type": "text", "selected_option_id": "sim", "selected_option_title": "Sim", "selected_option_type": "button"}},
		{"a tapped list row", func(t *testing.T) simClientMsg {
			return clientMsg(t, "reply_selection", map[string]string{"kind": "list", "id": "dir", "title": "Direito"})
		}, map[string]interface{}{"message": "Direito", "message_type": "text", "selected_option_id": "dir", "selected_option_title": "Direito", "selected_option_type": "list"}},
		{"a shared location", func(t *testing.T) simClientMsg {
			return clientMsg(t, "reply_location", map[string]interface{}{"latitude": -23.55, "longitude": -46.63, "name": "Escritório", "address": "Av. Paulista, 1000"})
		}, map[string]interface{}{"message": "📍 Escritório, Av. Paulista, 1000 (-23.550000, -46.630000)", "message_type": "text"}},
		{"a photo", func(t *testing.T) simClientMsg {
			return clientMsg(t, "reply_media", map[string]string{"kind": "image", "url": "https://cdn/x.jpg", "mime": "image/jpeg", "caption": "meu documento"})
		}, map[string]interface{}{"message": "meu documento", "message_type": "media", "media_type": "image"}},
		{"a document names its file", func(t *testing.T) simClientMsg {
			return clientMsg(t, "reply_media", map[string]string{"kind": "document", "url": "https://cdn/rg.pdf", "filename": "rg.pdf", "caption": "segue"})
		}, map[string]interface{}{"message": "rg.pdf", "message_type": "media", "media_type": "document"}},
		{"a voice note", func(t *testing.T) simClientMsg {
			return clientMsg(t, "reply_media", map[string]string{"kind": "audio", "url": "https://cdn/a.ogg", "caption": "quero me matricular"})
		}, map[string]interface{}{"message": "quero me matricular", "message_type": "audio", "media_type": "audio"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := parseSimInbound(tc.msg(t))
			if err != nil {
				t.Fatalf("parseSimInbound: %v", err)
			}
			if !reflect.DeepEqual(in.data, tc.want) {
				t.Fatalf("data = %v, want %v", in.data, tc.want)
			}
		})
	}
}

func TestATesterReplyTheWebhookWouldNotDeliverIsRefused(t *testing.T) {
	cases := []struct {
		name string
		kind string
		data interface{}
	}{
		{"empty text", "reply", map[string]string{"text": " "}},
		{"a selection without id", "reply_selection", map[string]string{"kind": "button", "title": "Sim"}},
		{"a selection without title", "reply_selection", map[string]string{"kind": "list", "id": "dir"}},
		{"an unknown selection kind", "reply_selection", map[string]string{"kind": "carousel", "id": "a", "title": "A"}},
		{"a location off the map", "reply_location", map[string]interface{}{"latitude": 123.0, "longitude": 10.0}},
		{"media without a file", "reply_media", map[string]string{"kind": "image"}},
		{"an unknown media kind", "reply_media", map[string]string{"kind": "gif", "url": "https://cdn/x.gif"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseSimInbound(clientMsg(t, tc.kind, tc.data)); err == nil {
				t.Fatal("the reply must be refused")
			}
		})
	}
}

func TestTheTesterRecordsTheReplyAsTheConversationWould(t *testing.T) {
	location, err := parseSimInbound(clientMsg(t, "reply_location", map[string]interface{}{"latitude": -23.55, "longitude": -46.63, "name": "Escritório"}))
	if err != nil {
		t.Fatal(err)
	}
	m := location.message("sim-entry")
	if found, ok := conversation.LocationOf(&m); !ok || found.Name != "Escritório" || m.ResolvedDirection() != conversation.MessageDirectionInbound {
		t.Fatalf("location message = %+v", m)
	}

	photo, err := parseSimInbound(clientMsg(t, "reply_media", map[string]string{"kind": "image", "url": "https://cdn/x.jpg", "mime": "image/jpeg", "caption": "foto"}))
	if err != nil {
		t.Fatal(err)
	}
	m = photo.message("sim-entry")
	if m.MessageType != conversation.MessageTypeMedia || m.MediaType != conversation.MediaTypeImage || m.Media == nil || m.Media.URL != "https://cdn/x.jpg" || m.Media.MimeType != "image/jpeg" || m.Text != "foto" {
		t.Fatalf("media message = %+v", m)
	}
}

func menuWorkflow() *workflow.Workflow {
	return &workflow.Workflow{ID: "wf-1", Graph: workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "menu", Type: workflow.NodeTypeActionSendInteractive},
			{ID: "grad", Type: workflow.NodeTypeActionSendText},
			{ID: "other", Type: workflow.NodeTypeActionSendText},
		},
		Edges: []workflow.Edge{{Source: "menu", Target: "grad", Label: "dir"}, {Source: "menu", Target: "other", Label: "no_match"}},
	}}
}

func waitingAt(node string) *workflow.WorkflowRun {
	return &workflow.WorkflowRun{ID: "run-1", CurrentNodeID: node, Status: workflow.RunStatusWaiting, WaitReason: workflow.WaitReasonReply, State: workflow.NewRunState()}
}

func TestATappedRowFollowsItsBranchLikeProduction(t *testing.T) {
	wf := menuWorkflow()
	in, err := parseSimInbound(clientMsg(t, "reply_selection", map[string]string{"kind": "list", "id": "dir", "title": "Direito"}))
	if err != nil {
		t.Fatal(err)
	}
	run := waitingAt("menu")
	if err := AdvanceOnReply(run, wf, in.replyData(wf.Graph.FindNode("menu"))); err != nil {
		t.Fatal(err)
	}
	if run.CurrentNodeID != "grad" || run.State.GetString("selected_option_type") != "list" || run.State.GetString("message") != "Direito" {
		t.Fatalf("run at %s with %v", run.CurrentNodeID, run.State.Vars)
	}

	typed, _ := parseSimInbound(clientMsg(t, "reply_media", map[string]string{"kind": "image", "url": "https://cdn/x.jpg"}))
	run = waitingAt("menu")
	if err := AdvanceOnReply(run, wf, typed.replyData(wf.Graph.FindNode("menu"))); err != nil || run.CurrentNodeID != "other" {
		t.Fatalf("a photo on a menu goes to no_match like production: at %s, err %v", run.CurrentNodeID, err)
	}
}

func TestAPlainReplyOnAMenuStillPicksTheOptionForOlderTesters(t *testing.T) {
	wf := menuWorkflow()
	in, err := parseSimInbound(clientMsg(t, "reply", map[string]string{"text": "dir"}))
	if err != nil {
		t.Fatal(err)
	}
	run := waitingAt("menu")
	if err := AdvanceOnReply(run, wf, in.replyData(wf.Graph.FindNode("menu"))); err != nil || run.CurrentNodeID != "grad" {
		t.Fatalf("at %s, err %v", run.CurrentNodeID, err)
	}
	if data := in.replyData(&workflow.Node{ID: "w", Type: workflow.NodeTypeWaitForReply}); data["selected_option_id"] != nil {
		t.Fatalf("a plain wait gets no selection: %v", data)
	}
}
