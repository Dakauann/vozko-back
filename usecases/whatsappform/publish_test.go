package whatsappform_usecase

import (
	"errors"
	"reflect"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
)

func outcomeOn(t *testing.T, result PublishResult, waba string) PublishOutcome {
	t.Helper()
	for _, o := range result.Outcomes {
		if o.MetaWABAID == waba {
			return o
		}
	}
	t.Fatalf("no outcome for %s in %+v", waba, result.Outcomes)
	return PublishOutcome{}
}

func TestPublishCreatesAndPublishesTheFlowOnEveryOwnedAccount(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "lead_capture")
	result, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.gateway.ops(); !reflect.DeepEqual(got, []string{"create:" + wabaA, "get:" + wabaA, "create:" + wabaB, "get:" + wabaB}) {
		t.Fatalf("gateway calls = %v", got)
	}
	for _, waba := range []string{wabaA, wabaB} {
		if o := outcomeOn(t, result, waba); o.Status != wf.PublicationPublished || o.Error != "" {
			t.Fatalf("outcome on %s = %+v", waba, o)
		}
		pub := w.pubs.only(t, waba)
		if pub.MetaFlowID == "" || pub.VersionID != detail.Draft.ID || pub.PublishedAt == nil || pub.MetaName != wf.MetaFlowName("Cadastro", detail.Form.ID) {
			t.Fatalf("publication on %s = %+v", waba, pub)
		}
	}
	if result.Detail.Form.PublishedVersionID != detail.Draft.ID {
		t.Fatalf("the form remembers the published version, got %+v", result.Detail.Form)
	}
	w.gateway.mu.Lock()
	create := w.gateway.calls[0]
	w.gateway.mu.Unlock()
	if !create.publish {
		t.Fatal("a valid form is created and published in one call")
	}
}

func TestPublishRefusesWithoutTheCapabilityOrWithAnInvalidDraft(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	reader := newWorld(t, wf.CapabilityView)
	reader.forms, reader.pubs = w.forms, w.pubs
	reader.build()
	if _, err := reader.svc.Publish(ctx, reader.viewer, detail.Form.ID); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	doc := detail.Draft.Document
	doc.Screens[0].Blocks[0].Field.Label = ""
	if _, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, detail.Draft.ID); err != nil {
		t.Fatal(err)
	}
	_, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	var problems wf.Problems
	if !errors.As(err, &problems) || len(problems) == 0 {
		t.Fatalf("an invalid draft is never sent to Meta, got %v", err)
	}
	if len(w.gateway.ops()) != 0 {
		t.Fatal("no Meta call for an invalid draft")
	}
}

func TestPublishRecheckBindingsAgainstTodaysCustomFields(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	doc := detail.Draft.Document
	doc.Screens[0].Blocks[0].Field.Binding = &wf.Binding{Target: wf.BindCustomField, CustomFieldKey: "obs"}
	w.defs.defs = append(w.defs.defs, newTextDefinition("obs"))
	w.build()
	saved, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, detail.Draft.ID)
	if err != nil || !saved.Compiled() {
		t.Fatalf("saved = %+v %v", saved, err)
	}
	w.defs.defs = w.defs.defs[:2]
	w.build()
	_, err = w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	var problems wf.Problems
	if !errors.As(err, &problems) || problems[0].Code != wf.ProblemBindingUnknown {
		t.Fatalf("a binding to a field deleted since the save must block publishing, got %v", err)
	}
}

func TestPublishWithoutAnOwnedOfficialNumber(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	w.phones.phones = []*businessphone.WhatsAppBusinessPhoneNumber{phone("p4", otherWS, "waba-other", businessphone.ProviderMeta)}
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); !errors.Is(err, wf.ErrNoEligibleAccount) {
		t.Fatalf("err = %v", err)
	}
}

func TestNumbersOutsideTheMetaProviderAreReportedNotPublished(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	w.phones.phones = append(w.phones.phones, phone("p360", ws, wabaD360, businessphone.ProviderDialog360))
	result, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o := outcomeOn(t, result, wabaD360); o.Code != wf.ErrorCode(wf.ErrProviderUnsupported) {
		t.Fatalf("outcome = %+v", o)
	}
	for _, op := range w.gateway.ops() {
		if op == "create:"+wabaD360 {
			t.Fatal("no Meta call on a provider that is not supported")
		}
	}
}

func TestDisconnectedNumbersDoNotMakeAnAccountEligible(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	for _, p := range w.phones.phones {
		if p.WABAId == wabaB {
			p.Status = businessphone.StatusDisconnected
		}
	}
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.gateway.ops(); !reflect.DeepEqual(got, []string{"create:" + wabaA, "get:" + wabaA}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestMetaValidationErrorsLandOnTheBlocks(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	w.gateway.createValid = []wf.MetaValidationError{{Message: "Label is too long", Paths: []string{"screens[0].layout.children[0].label"}}}
	result, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	if err != nil {
		t.Fatal(err)
	}
	o := outcomeOn(t, result, wabaA)
	if o.Status != wf.PublicationFailed || len(o.Problems) != 1 || o.Problems[0].Block != "b_f_nome" || o.Problems[0].Code != wf.ProblemMetaValidation {
		t.Fatalf("outcome = %+v", o)
	}
	if pub := w.pubs.only(t, wabaA); pub.MetaFlowID == "" || pub.Status != wf.PublicationFailed {
		t.Fatalf("the flow Meta created as a draft is kept for the next attempt, got %+v", pub)
	}
	if result.Detail.Form.PublishedVersionID != "" {
		t.Fatal("nothing was published")
	}
}

func TestRepublishUploadsTheNewVersionToTheSameFlow(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	first := w.pubs.only(t, wabaA)
	doc := detail.Draft.Document
	doc.Screens[0].Title = "Dados"
	next, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, detail.Draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.gateway.calls = nil
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.gateway.ops(); !reflect.DeepEqual(got, []string{"upload:" + wabaA, "publish:" + wabaA, "upload:" + wabaB, "publish:" + wabaB}) {
		t.Fatalf("calls = %v", got)
	}
	again := w.pubs.only(t, wabaA)
	if again.MetaFlowID != first.MetaFlowID || again.VersionID != next.ID {
		t.Fatalf("the same flow carries the new version, got %+v", again)
	}
}

func TestAnUploadMetaRefusesKeepsTheLiveVersion(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	doc := detail.Draft.Document
	doc.Screens[0].Title = "Dados"
	if _, err := w.svc.SaveDraft(ctx, w.viewer, detail.Form.ID, doc, detail.Draft.ID); err != nil {
		t.Fatal(err)
	}
	w.gateway.uploadValid = []wf.MetaValidationError{{Message: "bad"}}
	result, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o := outcomeOn(t, result, wabaA); len(o.Problems) != 1 || o.Status != wf.PublicationPublished {
		t.Fatalf("outcome = %+v", o)
	}
	if pub := w.pubs.only(t, wabaA); pub.VersionID != detail.Draft.ID || pub.Status != wf.PublicationPublished {
		t.Fatalf("the published version stays live, got %+v", pub)
	}
}

func TestAPublicationLeftCreatingIsRecoveredByNameNotCreatedTwice(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	w.gateway.createErr = errors.New("connection reset")
	result, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o := outcomeOn(t, result, wabaA); o.Error == "" || o.Status != wf.PublicationCreating {
		t.Fatalf("an ambiguous create leaves the publication creating, got %+v", o)
	}
	name := wf.MetaFlowName("Cadastro", detail.Form.ID)
	w.gateway.flows[wabaA+"/"+name] = wf.RemoteFlow{ID: "flow-meta-made", Name: name, Status: "DRAFT"}
	w.gateway.createErr = nil
	w.gateway.calls = nil
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	ops := w.gateway.ops()
	if ops[0] != "find:"+wabaA || ops[1] != "upload:"+wabaA || ops[2] != "publish:"+wabaA {
		t.Fatalf("calls = %v", ops)
	}
	if pub := w.pubs.only(t, wabaA); pub.MetaFlowID != "flow-meta-made" || pub.Status != wf.PublicationPublished {
		t.Fatalf("publication = %+v", pub)
	}
}

func TestRecoverStuckPublishesWhatAnInterruptedRunLeft(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	w.gateway.createErr = errors.New("timeout")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	w.gateway.createErr = nil
	w.now = w.now.Add(time.Hour)
	if err := w.svc.RecoverStuck(ctx); err != nil {
		t.Fatal(err)
	}
	for _, waba := range []string{wabaA, wabaB} {
		if pub := w.pubs.only(t, waba); pub.Status != wf.PublicationPublished {
			t.Fatalf("publication on %s = %+v", waba, pub)
		}
	}
}

func TestMetaRejectionsAreFailedOutcomes(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	w.gateway.createErr = &wf.MetaRejection{Code: 139000, Message: "Blocked by integrity"}
	result, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o := outcomeOn(t, result, wabaA); o.Status != wf.PublicationFailed || o.Code != wf.ErrorCode(wf.ErrMetaRejected) {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestDeprecate(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Deprecate(ctx, w.viewer, detail.Form.ID, wabaA); err != nil {
		t.Fatal(err)
	}
	if pub := w.pubs.only(t, wabaA); pub.Status != wf.PublicationDeprecated {
		t.Fatalf("publication = %+v", pub)
	}
	if err := w.svc.Deprecate(ctx, w.viewer, detail.Form.ID, "waba-none"); !errors.Is(err, wf.ErrPublicationNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestArchiveDeprecatesEveryLivePublicationFirst(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	w.gateway.deprecErr = errors.New("meta down")
	if err := w.svc.Archive(ctx, w.viewer, detail.Form.ID); err == nil {
		t.Fatal("a form whose flow Meta still serves is never archived")
	}
	if f, _ := w.forms.Get(ctx, ws, detail.Form.ID); f.ArchivedAt != nil {
		t.Fatal("the archive must not happen when deprecating failed")
	}
	w.gateway.deprecErr = nil
	if err := w.svc.Archive(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	for _, waba := range []string{wabaA, wabaB} {
		if pub := w.pubs.only(t, waba); pub.Status != wf.PublicationDeprecated {
			t.Fatalf("publication on %s = %+v", waba, pub)
		}
	}
}

func TestPreviewUsesItsOwnDraftFlow(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	preview, err := w.svc.Preview(ctx, w.viewer, detail.Form.ID)
	if err != nil || preview.URL == "" || preview.ExpiresAt == nil {
		t.Fatalf("preview = %+v %v", preview, err)
	}
	w.gateway.mu.Lock()
	create := w.gateway.calls[1]
	w.gateway.mu.Unlock()
	if create.op != "create" || create.publish || create.name != wf.PreviewFlowName(detail.Form.ID) {
		t.Fatalf("the preview flow is a separate draft, got %+v", create)
	}
	w.gateway.calls = nil
	if _, err := w.svc.Preview(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	if ops := w.gateway.ops(); ops[0] != "find:"+wabaA || ops[1] != "upload:"+wabaA || ops[2] != "preview:"+wabaA {
		t.Fatalf("a second preview reuses the draft flow, got %v", ops)
	}
	if len(w.pubs.rows) != 0 {
		t.Fatal("a preview never creates a publication")
	}
}

func TestReconcilePublishesThePublishedVersionOnANewAccount(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	w.phones.phones = append(w.phones.phones, phone("p9", ws, "waba-new", businessphone.ProviderMeta))
	if err := w.svc.Reconcile(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if pub := w.pubs.only(t, "waba-new"); pub.Status != wf.PublicationPublished || pub.VersionID != detail.Draft.ID {
		t.Fatalf("publication = %+v", pub)
	}
}

func TestReconcileAllVisitsEveryWorkspaceWithAPublishedForm(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	w.phones.phones = append(w.phones.phones, phone("p9", ws, "waba-new", businessphone.ProviderMeta))
	if err := w.svc.ReconcileAll(ctx); err != nil {
		t.Fatal(err)
	}
	if pub := w.pubs.only(t, "waba-new"); pub.Status != wf.PublicationPublished {
		t.Fatalf("publication = %+v", pub)
	}
}

func TestPublishTargetsNameTheAccountsAPublishWillReach(t *testing.T) {
	w := builder(t)
	w.phones.phones = append(w.phones.phones, phone("p360", ws, wabaD360, businessphone.ProviderDialog360))
	targets, err := w.svc.PublishTargets(ctx, w.viewer)
	if err != nil {
		t.Fatal(err)
	}
	want := []PublishTarget{{MetaWABAID: wabaD360}, {MetaWABAID: wabaA, Supported: true}, {MetaWABAID: wabaB, Supported: true}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %+v", targets)
	}
	reader := newWorld(t, wf.CapabilityView)
	if _, err := reader.svc.PublishTargets(ctx, reader.viewer); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}
