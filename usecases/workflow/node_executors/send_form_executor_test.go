package node_executors

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workflow"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type recordingFormSender struct {
	sends []whatsappform_usecase.WorkflowSend
	err   error
}

func (s *recordingFormSender) SendFromWorkflow(_ context.Context, in whatsappform_usecase.WorkflowSend) (wf.Send, error) {
	s.sends = append(s.sends, in)
	if s.err != nil {
		return wf.Send{ID: "send-failed"}, s.err
	}
	return wf.Send{ID: "send-1", MessageID: "wamid.form", FormID: in.FormID, WorkflowRunID: in.RunID}, nil
}

type recordedTemplateSend struct {
	templateID string
	params     map[string]string
	runID      string
}

type recordingTemplateFallback struct {
	sends []recordedTemplateSend
	err   error
}

func (r *recordingTemplateFallback) SendTemplate(_ context.Context, run *workflow.WorkflowRun, templateID, _ string, params map[string]string, state *workflow.RunState) (*conversation.SendTextMessageOutput, string, error) {
	resolved := map[string]string{}
	for k, v := range params {
		resolved[k] = workflow.Interpolate(v, state, nil)
	}
	r.sends = append(r.sends, recordedTemplateSend{templateID: templateID, params: resolved, runID: run.ID})
	if r.err != nil {
		return nil, "phone-1", r.err
	}
	return &conversation.SendTextMessageOutput{MessageID: "wamid.template"}, "phone-1", nil
}

func formExecutorWith(forms FormSender) *sendFormExecutor {
	return &sendFormExecutor{forms: forms}
}

func formEdges(labels ...string) []workflow.Edge {
	edges := make([]workflow.Edge, 0, len(labels))
	for _, label := range labels {
		edges = append(edges, workflow.Edge{Source: "n1", Target: "to_" + label, Label: label})
	}
	return edges
}

func formNodeCtx(entryType shared.EntryType, config map[string]interface{}, edges []workflow.Edge) *workflow.NodeContext {
	state := workflow.NewRunState()
	state.Set("name", "Ana")
	return &workflow.NodeContext{
		Node:  &workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendForm, Config: config},
		Graph: &workflow.Graph{Edges: edges},
		Run:   &workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: string(entryType)},
		State: &state,
	}
}

func formConfig() map[string]interface{} {
	return map[string]interface{}{
		workflow.FormNodeFormIDKey: "form-1",
		"header":                   "Cadastro",
		"body":                     "Oi {{name}}, preencha por favor",
		"footer":                   "Leva 1 minuto",
		"cta":                      "Abrir",
		"timeout_seconds":          float64(600),
	}
}

func TestTheFormNodeSendsTheFormAndParksUntilTheAnswer(t *testing.T) {
	cases := []struct {
		name    string
		config  func() map[string]interface{}
		timeout time.Duration
	}{
		{"with its own timeout", formConfig, 600 * time.Second},
		{"with the default timeout", func() map[string]interface{} {
			cfg := formConfig()
			delete(cfg, "timeout_seconds")
			return cfg
		}, interactiveDefaultTimeoutSeconds * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &recordingFormSender{}
			ctx := formNodeCtx(shared.EntryTypeWhatsApp, tc.config(), formEdges(workflow.FormHandleCompleted, workflow.FormHandleNoReply))
			before := time.Now().UTC().Truncate(time.Millisecond)

			result, err := formExecutorWith(sender).Execute(ctx)

			if err != nil {
				t.Fatal(err)
			}
			if len(sender.sends) != 1 {
				t.Fatalf("sends = %+v", sender.sends)
			}
			got := sender.sends[0]
			want := whatsappform_usecase.WorkflowSend{
				WorkspaceID: "ws-1", EntryID: "entry-1", FormID: "form-1", RunID: "run-1",
				Copy: wf.SendCopy{Header: "Cadastro", Body: "Oi Ana, preencha por favor", Footer: "Leva 1 minuto", CTA: "Abrir"},
			}
			if got != want {
				t.Fatalf("send = %+v, want %+v", got, want)
			}
			if result.Wait == nil || result.Wait.Reason != workflow.WaitReasonForm || result.NextNodeID != "" {
				t.Fatalf("result = %+v", result)
			}
			wake := time.UnixMilli(result.Wait.WakeAt)
			if wake.Before(before.Add(tc.timeout)) || wake.After(time.Now().UTC().Add(tc.timeout)) {
				t.Fatalf("wake at %v, want about %v from now", wake, tc.timeout)
			}
			if result.Output["sent"] != true || result.Output["send_id"] != "send-1" || result.Output["message_id"] != "wamid.form" || result.Output[workflow.FormNodeFormIDKey] != "form-1" {
				t.Fatalf("output = %+v", result.Output)
			}
		})
	}
}

func TestAFormThatCannotBeSentFollowsSendFailedOrFailsTheStep(t *testing.T) {
	refusals := []error{wf.ErrOutsideWindow, wf.ErrNoPublication, wf.ErrPublicationNotSendable, wf.ErrCopyInvalid, errors.New("meta refused")}
	for _, refusal := range refusals {
		t.Run(refusal.Error(), func(t *testing.T) {
			wired := formNodeCtx(shared.EntryTypeWhatsApp, formConfig(), formEdges(workflow.FormHandleCompleted, workflow.FormHandleSendFailed))
			result, err := formExecutorWith(&recordingFormSender{err: refusal}).Execute(wired)
			if err != nil {
				t.Fatal(err)
			}
			if result.NextNodeID != "to_send_failed" || result.Wait != nil || result.Output["sent"] != false {
				t.Fatalf("result = %+v", result)
			}
			if code := wf.ErrorCode(refusal); code != "" && result.Output["reason"] != code {
				t.Fatalf("reason = %v, want %s", result.Output["reason"], code)
			}

			unwired := formNodeCtx(shared.EntryTypeWhatsApp, formConfig(), formEdges(workflow.FormHandleCompleted))
			if _, err := formExecutorWith(&recordingFormSender{err: refusal}).Execute(unwired); !errors.Is(err, refusal) {
				t.Fatalf("err = %v, want %v", err, refusal)
			}
		})
	}
}

func fallbackConfig() map[string]interface{} {
	cfg := formConfig()
	cfg[formFallbackTemplateKey] = "tmpl-form"
	cfg[formFallbackParamsKey] = map[string]interface{}{"1": "{{name}}"}
	return cfg
}

func TestAClosedWindowSendsTheFallbackTemplateAndStillWaitsForTheForm(t *testing.T) {
	templates := &recordingTemplateFallback{}
	exec := &sendFormExecutor{forms: &recordingFormSender{err: wf.ErrOutsideWindow}, templates: templates}
	ctx := formNodeCtx(shared.EntryTypeWhatsApp, fallbackConfig(), formEdges(workflow.FormHandleCompleted, workflow.FormHandleSendFailed))

	result, err := exec.Execute(ctx)

	if err != nil {
		t.Fatal(err)
	}
	if len(templates.sends) != 1 || templates.sends[0].templateID != "tmpl-form" || templates.sends[0].runID != "run-1" || templates.sends[0].params["1"] != "Ana" {
		t.Fatalf("template sends = %+v", templates.sends)
	}
	if result.Wait == nil || result.Wait.Reason != workflow.WaitReasonForm || result.NextNodeID != "" {
		t.Fatalf("result = %+v", result)
	}
	if result.Output["sent"] != true || result.Output["message_id"] != "wamid.template" || result.Output[formFallbackTemplateKey] != "tmpl-form" {
		t.Fatalf("output = %+v", result.Output)
	}
}

func TestTheFallbackTemplateOnlyCoversAClosedWindow(t *testing.T) {
	cases := []struct {
		name          string
		formErr       error
		templates     *recordingTemplateFallback
		wantTemplates int
	}{
		{"another refusal", wf.ErrNoPublication, &recordingTemplateFallback{}, 0},
		{"the template fails too", wf.ErrOutsideWindow, &recordingTemplateFallback{err: errors.New("template refused")}, 1},
		{"no template sender", wf.ErrOutsideWindow, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &sendFormExecutor{forms: &recordingFormSender{err: tc.formErr}}
			if tc.templates != nil {
				exec.templates = tc.templates
			}
			wired := formNodeCtx(shared.EntryTypeWhatsApp, fallbackConfig(), formEdges(workflow.FormHandleCompleted, workflow.FormHandleSendFailed))
			result, err := exec.Execute(wired)
			if err != nil {
				t.Fatal(err)
			}
			if result.NextNodeID != "to_send_failed" || result.Wait != nil || result.Output["sent"] != false {
				t.Fatalf("result = %+v", result)
			}
			if tc.templates != nil && len(tc.templates.sends) != tc.wantTemplates {
				t.Fatalf("template sends = %+v", tc.templates.sends)
			}

			unwired := formNodeCtx(shared.EntryTypeWhatsApp, fallbackConfig(), formEdges(workflow.FormHandleCompleted))
			if _, err := exec.Execute(unwired); err == nil {
				t.Fatal("an unsent form without send_failed must fail the step")
			}
		})
	}
}

func TestTheFormNodeOnlySendsOnOfficialWhatsApp(t *testing.T) {
	for _, entryType := range []shared.EntryType{shared.EntryTypeInstagram, shared.EntryType("unofficial_whatsapp"), shared.EntryType("telegram")} {
		t.Run(string(entryType), func(t *testing.T) {
			sender := &recordingFormSender{}
			wired := formNodeCtx(entryType, formConfig(), formEdges(workflow.FormHandleCompleted, workflow.FormHandleSendFailed))
			result, err := formExecutorWith(sender).Execute(wired)
			if err != nil {
				t.Fatal(err)
			}
			if result.NextNodeID != "to_send_failed" || result.Output["skipped"] != true || result.Output["sent"] != false {
				t.Fatalf("result = %+v", result)
			}

			unwired := formNodeCtx(entryType, formConfig(), formEdges(workflow.FormHandleCompleted))
			if _, err := formExecutorWith(sender).Execute(unwired); !errors.Is(err, wf.ErrChannelUnsupported) {
				t.Fatalf("err = %v", err)
			}
			if len(sender.sends) != 0 {
				t.Fatalf("a form was sent on %s: %+v", entryType, sender.sends)
			}
		})
	}
}

func TestTheFormNodeRefusesWithoutAFormOrASender(t *testing.T) {
	noForm := formConfig()
	delete(noForm, workflow.FormNodeFormIDKey)
	if _, err := formExecutorWith(&recordingFormSender{}).Execute(formNodeCtx(shared.EntryTypeWhatsApp, noForm, formEdges(workflow.FormHandleSendFailed))); !errors.Is(err, workflow.ErrNodeConfigMissing) {
		t.Fatalf("err = %v", err)
	}

	result, err := formExecutorWith(nil).Execute(formNodeCtx(shared.EntryTypeWhatsApp, formConfig(), formEdges(workflow.FormHandleSendFailed)))
	if err != nil || result.NextNodeID != "to_send_failed" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if _, err := formExecutorWith(nil).Execute(formNodeCtx(shared.EntryTypeWhatsApp, formConfig(), nil)); !errors.Is(err, ErrFormSenderUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheFormNodeDefinition(t *testing.T) {
	def := NewSendFormExecutor(nil, SenderDeps{}).Definition()
	if def.Type != workflow.NodeTypeActionSendForm || def.Label != "Enviar formulário" || !def.Type.Valid() {
		t.Fatalf("definition = %+v", def)
	}
	if !workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeMessages) || workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeVoice) {
		t.Fatal("the form node belongs to messages workflows only")
	}
	outputs := map[string]bool{}
	for _, h := range def.Outputs {
		outputs[h.ID] = h.Optional
	}
	if optional, ok := outputs[workflow.FormHandleCompleted]; !ok || optional {
		t.Fatalf("completed must be a required output: %+v", def.Outputs)
	}
	for _, id := range []string{workflow.FormHandleNoReply, workflow.FormHandleSendFailed} {
		if optional, ok := outputs[id]; !ok || !optional {
			t.Fatalf("%s must be an optional output: %+v", id, def.Outputs)
		}
	}
	fields := map[string]workflow.ConfigField{}
	for _, f := range def.ConfigSchema {
		fields[f.Key] = f
	}
	for _, key := range []string{workflow.FormNodeFormIDKey, "body", "cta"} {
		if !fields[key].Required {
			t.Errorf("%s must be required", key)
		}
	}
	for _, key := range []string{"header", "footer", "timeout_seconds", formFallbackTemplateKey, formFallbackParamsKey} {
		if _, ok := fields[key]; !ok || fields[key].Required {
			t.Errorf("%s must be an optional field", key)
		}
	}
	if fields[workflow.FormNodeFormIDKey].OptionsSource != "whatsapp_forms" {
		t.Errorf("form picker = %+v", fields[workflow.FormNodeFormIDKey])
	}
	if fields[formFallbackTemplateKey].OptionsSource != "whatsapp_templates" || fields[formFallbackParamsKey].Type != "keyvalue" {
		t.Errorf("fallback fields = %+v, %+v", fields[formFallbackTemplateKey], fields[formFallbackParamsKey])
	}
}

func TestTheFormSenderRelayFailsUntilBound(t *testing.T) {
	relay := NewFormSenderRelay()
	if _, err := relay.SendFromWorkflow(context.Background(), whatsappform_usecase.WorkflowSend{}); !errors.Is(err, ErrFormSenderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	target := &recordingFormSender{}
	relay.Bind(target)
	send, err := relay.SendFromWorkflow(context.Background(), whatsappform_usecase.WorkflowSend{FormID: "form-1", RunID: "run-1"})
	if err != nil || send.ID != "send-1" || len(target.sends) != 1 {
		t.Fatalf("send = %+v, err = %v", send, err)
	}
}
