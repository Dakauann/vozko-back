package node_executors

import (
	"reflect"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func customerCtx(nodeType workflow.NodeType, config map[string]interface{}, run *workflow.WorkflowRun) *workflow.NodeContext {
	state := workflow.NewRunState()
	state.Set("input", map[string]interface{}{"nome": "Ana", "ativo": true, "nascimento": "1990-05-12"})
	return &workflow.NodeContext{
		Run:   run,
		Node:  &workflow.Node{ID: "n1", Type: nodeType, Config: config},
		Graph: &workflow.Graph{},
		State: &state,
		Trace: &workflow.StepTrace{},
	}
}

func instagramSender(adapter *fakeAdapter) *channelSender {
	return &channelSender{
		adapters:   conversation.NewAdapterRegistry(adapter),
		entryLeads: &entryLeads{}, leads: &knownLeadRepo{},
		history: &recordingHistory{},
	}
}

func TestASentTextNeverCarriesTemplateSyntaxAndNamesWhatWasMissing(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	ctx := customerCtx(workflow.NodeTypeActionSendText, map[string]interface{}{
		"text": "Oi {{input.nome}}! Ativo: {{input.ativo}}. Nasceu em {{input.nascimento}}. Fone: {{input.telefone}}",
	}, igRun())

	result, err := (&sendTextExecutor{sender: instagramSender(adapter)}).Execute(ctx)

	if err != nil {
		t.Fatal(err)
	}
	want := "Oi Ana! Ativo: Sim. Nasceu em 12/05/1990. Fone: "
	if adapter.sentBody != want || result.Output["text"] != want {
		t.Fatalf("sent %q output %v, want %q", adapter.sentBody, result.Output["text"], want)
	}
	if got := ctx.Trace.Unresolved(); !reflect.DeepEqual(got, []string{"input.telefone"}) {
		t.Fatalf("unresolved = %v", got)
	}
}

func TestAMediaCaptionIsCustomerText(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	ctx := customerCtx(workflow.NodeTypeActionSendMedia, map[string]interface{}{
		"media_url": "https://cdn.exemplo.com/a.png",
		"caption":   "Para {{input.nome}} {{lead_name}}",
	}, igRun())
	executor := &sendMediaExecutor{sender: &whatsappSender{deps: SenderDeps{MediaURLs: externalOnlyURLs()}}, channel: instagramSender(adapter)}

	if res, err := executor.Execute(ctx); err != nil || res.Output["sent"] != true {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if adapter.sentMedia.Caption != "Para Ana " {
		t.Fatalf("caption = %q", adapter.sentMedia.Caption)
	}
	if got := ctx.Trace.Unresolved(); !reflect.DeepEqual(got, []string{"lead_name"}) {
		t.Fatalf("unresolved = %v", got)
	}
}

func TestTemplateParametersAreCustomerText(t *testing.T) {
	ctx := customerCtx(workflow.NodeTypeActionSendTemplate, map[string]interface{}{
		"params": map[string]interface{}{"1": "{{input.nome}}", "2": "{{input.ativo}}", "3": "{{input.cpf}}", "4": 2.5, "5": nil},
	}, igRun())

	got := templateParamValues(ctx, "params")

	want := map[string]string{"1": "Ana", "2": "Sim", "3": "", "4": "2,5", "5": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params = %v, want %v", got, want)
	}
	if refs := ctx.Trace.Unresolved(); !reflect.DeepEqual(refs, []string{"input.cpf"}) {
		t.Fatalf("unresolved = %v", refs)
	}
}

func TestInteractiveOptionsAreCustomerText(t *testing.T) {
	ctx := customerCtx(workflow.NodeTypeActionSendInteractive, nil, igRun())
	config := importedConfig(t, `{"interactive_type":"buttons","buttons":[{"id":"a","title":"Sou {{input.nome}}"},{"id":"b","title":"{{input.apelido}}"}]}`)

	options, err := interactiveOptionsOf(config, customerTextIn(ctx))

	if err != nil {
		t.Fatal(err)
	}
	if options[0].Title != "Sou Ana" || options[1].Title != "" {
		t.Fatalf("options = %+v", options)
	}
	if refs := ctx.Trace.Unresolved(); !reflect.DeepEqual(refs, []string{"input.apelido"}) {
		t.Fatalf("unresolved = %v", refs)
	}
}

func TestTheFormCopyIsCustomerText(t *testing.T) {
	sender := &recordingFormSender{}
	ctx := customerCtx(workflow.NodeTypeActionSendForm, map[string]interface{}{
		workflow.FormNodeFormIDKey: "form-1",
		"body":                     "Oi {{input.nome}} {{input.sobrenome}}",
	}, &workflow.WorkflowRun{ID: "run-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: string(shared.EntryTypeWhatsApp)})
	ctx.Graph = &workflow.Graph{Edges: formEdges(workflow.FormHandleCompleted)}

	if _, err := formExecutorWith(sender).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if body := sender.sends[0].Copy.Body; body != "Oi Ana " {
		t.Fatalf("body = %q", body)
	}
	if refs := ctx.Trace.Unresolved(); !reflect.DeepEqual(refs, []string{"input.sobrenome"}) {
		t.Fatalf("unresolved = %v", refs)
	}
}

func TestAISegmentsReachTheCustomerWithoutTemplateSyntax(t *testing.T) {
	ctx := customerCtx(workflow.NodeTypeActionAIAgent, nil, igRun())

	got := customerSegments(ctx, []string{"Olá {{input.nome}}", "Seu protocolo é {{protocolo}}"})

	if !reflect.DeepEqual(got, []string{"Olá Ana", "Seu protocolo é "}) {
		t.Fatalf("segments = %v", got)
	}
	if refs := ctx.Trace.Unresolved(); !reflect.DeepEqual(refs, []string{"protocolo"}) {
		t.Fatalf("unresolved = %v", refs)
	}
}
