package template_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

// createMockWAClient embeds the full client mock and captures the
// CreateTemplate input so we can assert exactly what parameter_format the use
// case forwards to Meta, the field whose absence caused named templates to be
// rejected with INVALID_FORMAT.
type createMockWAClient struct {
	syncTemplatesClientMock
	createInput  *conversation.CreateTemplateInput
	createOutput *conversation.CreateTemplateOutput
}

func (m *createMockWAClient) CreateTemplate(_ context.Context, input conversation.CreateTemplateInput) (*conversation.CreateTemplateOutput, error) {
	m.createInput = &input
	if m.createOutput != nil {
		return m.createOutput, nil
	}
	return &conversation.CreateTemplateOutput{ID: "ext-created", Status: "PENDING"}, nil
}

// fakeSetHeaderMediaUC stands in for the reused SetTemplateHeaderMediaUseCase so
// create tests can assert the WhatsApp media id is minted without hitting the
// network. A default (no-op) instance leaves non-media tests unaffected, since
// create only invokes it for a media header with a URL.
type fakeSetHeaderMediaUC struct {
	calls []template.SetTemplateHeaderMediaInput
	err   error
}

func (f *fakeSetHeaderMediaUC) Execute(in template.SetTemplateHeaderMediaInput) error {
	f.calls = append(f.calls, in)
	return f.err
}

func newCreateUC(client *createMockWAClient) template.CreateTemplateUseCase {
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	return NewCreateTemplateUseCase(factory, &sendMockTemplateRepo{}, &fakeSetHeaderMediaUC{})
}

func namedBodyComponent() template.TemplateComponent {
	return template.TemplateComponent{
		Type: "BODY",
		Text: "Olá {{nome}}, sua ordem foi enviada",
		Example: &template.TemplateExample{
			BodyTextNamed: []template.NamedParamExample{{ParamName: "nome", Example: "Maria"}},
		},
	}
}

func positionalBodyComponent() template.TemplateComponent {
	return template.TemplateComponent{
		Type:    "BODY",
		Text:    "Olá {{1}}, sua ordem foi enviada",
		Example: &template.TemplateExample{BodyText: [][]string{{"Maria"}}},
	}
}

func baseCreateInput(comps ...template.TemplateComponent) template.CreateTemplateInput {
	return template.CreateTemplateInput{
		BusinessPhoneID: "phone-1",
		Name:            "ordem_feita",
		Language:        "pt_BR",
		Category:        template.TemplateCategoryUtility,
		Components:      comps,
	}
}

// Named placeholders ({{nome}}) must be sent to Meta with parameter_format=NAMED.
// This is the exact defect: the app historically sent no parameter_format, so
// Meta parsed the named placeholder as positional and rejected INVALID_FORMAT.
func TestCreateTemplate_NamedParams_SendsNamedFormat(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	_, err := uc.Execute(baseCreateInput(namedBodyComponent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.createInput == nil {
		t.Fatal("expected CreateTemplate to be called")
	}
	if got := client.createInput.ParameterFormat; got != "NAMED" {
		t.Errorf("parameter_format = %q, want NAMED", got)
	}
}

func TestCreateTemplate_PositionalParams_SendsPositionalFormat(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	_, err := uc.Execute(baseCreateInput(positionalBodyComponent()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.createInput.ParameterFormat; got != "POSITIONAL" {
		t.Errorf("parameter_format = %q, want POSITIONAL", got)
	}
}

func TestCreateTemplate_NoParams_SendsPositionalFormat(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	body := template.TemplateComponent{Type: "BODY", Text: "Sua ordem foi enviada"}
	_, err := uc.Execute(baseCreateInput(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.createInput.ParameterFormat; got != "POSITIONAL" {
		t.Errorf("parameter_format = %q, want POSITIONAL for a no-variable template", got)
	}
}

// The body is the source of truth: even if the client supplies a wrong/stale
// ParameterFormat, the use case infers from the actual placeholders.
func TestCreateTemplate_NamedParams_OverridesWrongClientFormat(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	in := baseCreateInput(namedBodyComponent())
	in.ParameterFormat = "positional" // wrong on purpose
	_, err := uc.Execute(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.createInput.ParameterFormat; got != "NAMED" {
		t.Errorf("parameter_format = %q, want NAMED (backend must override the client hint)", got)
	}
}

// Mixing numbered ({{1}}) and named ({{nome}}) placeholders is rejected before
// ever reaching Meta, it would otherwise come back as INVALID_FORMAT.
func TestCreateTemplate_MixedFormat_Rejected(t *testing.T) {
	client := &createMockWAClient{}
	uc := newCreateUC(client)

	mixed := template.TemplateComponent{
		Type: "BODY",
		Text: "Olá {{nome}}, sua ordem {{1}} foi enviada",
		Example: &template.TemplateExample{
			BodyTextNamed: []template.NamedParamExample{{ParamName: "nome", Example: "Maria"}},
		},
	}
	_, err := uc.Execute(baseCreateInput(mixed))
	if !errors.Is(err, template.ErrMixedParameterStyles) {
		t.Fatalf("expected ErrMixedParameterStyles, got %v", err)
	}
	if client.createInput != nil {
		t.Error("Meta must not be called when the template fails validation")
	}
}

// A call-permission template with named params must also carry NAMED, and a
// rejection reason from Meta must surface in the use-case output.
func TestCreateTemplate_RejectedReason_Threaded(t *testing.T) {
	client := &createMockWAClient{
		createOutput: &conversation.CreateTemplateOutput{
			ID:             "ext-rej",
			Status:         "REJECTED",
			RejectedReason: "INVALID_FORMAT",
		},
	}
	uc := newCreateUC(client)

	out, err := uc.Execute(baseCreateInput(
		namedBodyComponent(),
		template.TemplateComponent{Type: "CALL_PERMISSION_REQUEST"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.createInput.ParameterFormat != "NAMED" {
		t.Errorf("parameter_format = %q, want NAMED", client.createInput.ParameterFormat)
	}
	if out.Status != template.TemplateStatusRejected {
		t.Errorf("status = %q, want REJECTED", out.Status)
	}
	if out.RejectedReason != "INVALID_FORMAT" {
		t.Errorf("rejectedReason = %q, want INVALID_FORMAT", out.RejectedReason)
	}
}

// mediaHeaderMockClient captures the create payload and records whether the use
// case uploaded the header media to a Resumable-Upload handle. wantsURL mirrors
// the concrete client's TemplateHeaderMediaWantsURL: true for 360dialog (which
// wants the public URL verbatim in header_handle), false for Meta (which wants an
// uploaded handle).
type mediaHeaderMockClient struct {
	createMockWAClient
	wantsURL    bool
	uploadCalls int
	uploadedURL string
}

func (m *mediaHeaderMockClient) TemplateHeaderMediaWantsURL() bool { return m.wantsURL }

func (m *mediaHeaderMockClient) UploadMediaForTemplate(_ context.Context, in conversation.UploadMediaForTemplateInput) (string, error) {
	m.uploadCalls++
	m.uploadedURL = in.URL
	return "4::handle::uploaded", nil
}

func mediaHeaderComponent(url string) template.TemplateComponent {
	return template.TemplateComponent{
		Type:    "HEADER",
		Format:  "IMAGE",
		Example: &template.TemplateExample{HeaderHandle: []string{url}},
	}
}

func firstHeaderHandle(t *testing.T, in *conversation.CreateTemplateInput) string {
	t.Helper()
	if in == nil {
		t.Fatal("CreateTemplate was not called")
	}
	for _, c := range in.Components {
		if c.Example != nil && len(c.Example.HeaderHandle) > 0 {
			return c.Example.HeaderHandle[0]
		}
	}
	t.Fatal("no header_handle found in create payload")
	return ""
}

// 360dialog's channel-scoped template endpoint fetches the header media from the
// URL itself and rejects an uploaded handle with 400 "it should be valid url
// address". So for those channels the raw URL must be passed straight through in
// header_handle, the use case must NOT upload it to a handle first.
func TestCreateTemplate_Dialog360_MediaHeaderPassesURLThrough(t *testing.T) {
	const url = "https://discador.net/img/enioalmeida/enioalmeida.jpg"
	client := &mediaHeaderMockClient{wantsURL: true}
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	uc := NewCreateTemplateUseCase(factory, &sendMockTemplateRepo{}, &fakeSetHeaderMediaUC{})

	if _, err := uc.Execute(baseCreateInput(mediaHeaderComponent(url), positionalBodyComponent())); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.uploadCalls != 0 {
		t.Errorf("UploadMediaForTemplate called %d times; 360dialog must pass the URL through, not upload it", client.uploadCalls)
	}
	if got := firstHeaderHandle(t, client.createInput); got != url {
		t.Errorf("header_handle = %q, want the raw URL %q", got, url)
	}
}

// Meta's Graph endpoint requires a Resumable-Upload handle, so the URL must still
// be uploaded and substituted for the Meta provider (no / false capability).
func TestCreateTemplate_Meta_MediaHeaderUploadedToHandle(t *testing.T) {
	const url = "https://discador.net/img/enioalmeida/enioalmeida.jpg"
	client := &mediaHeaderMockClient{wantsURL: false}
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	uc := NewCreateTemplateUseCase(factory, &sendMockTemplateRepo{}, &fakeSetHeaderMediaUC{})

	if _, err := uc.Execute(baseCreateInput(mediaHeaderComponent(url), positionalBodyComponent())); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.uploadCalls != 1 {
		t.Fatalf("UploadMediaForTemplate called %d times, want 1 (Meta needs an uploaded handle)", client.uploadCalls)
	}
	if client.uploadedURL != url {
		t.Errorf("uploaded URL = %q, want %q", client.uploadedURL, url)
	}
	if got := firstHeaderHandle(t, client.createInput); got != "4::handle::uploaded" {
		t.Errorf("header_handle = %q, want the uploaded handle", got)
	}
}

// A media-header template must have its WhatsApp media id minted at create time
// (every campaign/workflow/tool send path attaches the header by id, not URL).
// Create must delegate that to the shared SetTemplateHeaderMediaUseCase, keyed by
// the persisted template id and the provided header URL.
func TestCreateTemplate_MediaHeader_MintsMediaIDViaSetHeaderMedia(t *testing.T) {
	const url = "https://discador.net/img/enioalmeida/enioalmeida.jpg"
	client := &mediaHeaderMockClient{wantsURL: true} // wantsURL avoids the network in processHeaderMediaURLs
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	repo := &capturingTemplateRepo{}
	setHeaderMedia := &fakeSetHeaderMediaUC{}
	uc := NewCreateTemplateUseCase(factory, repo, setHeaderMedia)

	in := baseCreateInput(mediaHeaderComponent(url), positionalBodyComponent())
	headerURL := url
	in.HeaderMediaURL = &headerURL

	if _, err := uc.Execute(in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.created == nil {
		t.Fatal("expected template to be persisted")
	}
	if len(setHeaderMedia.calls) != 1 {
		t.Fatalf("SetTemplateHeaderMedia called %d times, want 1", len(setHeaderMedia.calls))
	}
	got := setHeaderMedia.calls[0]
	if got.TemplateID != repo.created.ID {
		t.Errorf("minted media id for template %q, want the just-created %q", got.TemplateID, repo.created.ID)
	}
	if got.HeaderMediaURL == nil || *got.HeaderMediaURL != url {
		t.Errorf("HeaderMediaURL = %v, want %q", got.HeaderMediaURL, url)
	}
}

// A non-media template must never trigger header-media minting.
func TestCreateTemplate_NoMediaHeader_SkipsHeaderMediaMinting(t *testing.T) {
	client := &createMockWAClient{}
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	setHeaderMedia := &fakeSetHeaderMediaUC{}
	uc := NewCreateTemplateUseCase(factory, &sendMockTemplateRepo{}, setHeaderMedia)

	if _, err := uc.Execute(baseCreateInput(positionalBodyComponent())); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(setHeaderMedia.calls) != 0 {
		t.Errorf("SetTemplateHeaderMedia called %d times for a text-only template, want 0", len(setHeaderMedia.calls))
	}
}

// capturingTemplateRepo records the persisted template so tests can assert what
// actually landed in storage (the shared sendMockTemplateRepo.Create discards it).
type capturingTemplateRepo struct {
	sendMockTemplateRepo
	created *template.Template
}

func (r *capturingTemplateRepo) Create(t *template.Template) error {
	r.created = t
	return nil
}

// 360dialog's channel-scoped create endpoint returns the status lowercased
// ("pending"/"approved"/"rejected"). It must be normalised to the uppercase
// domain constants, otherwise IsApproved/CanSend never recognise the template
// and it can never be sent even after Meta approves it.
func TestCreateTemplate_Dialog360LowercaseStatus_NormalizedToUppercase(t *testing.T) {
	cases := []struct {
		raw  string
		want template.TemplateStatus
	}{
		{"pending", template.TemplateStatusPending},
		{"approved", template.TemplateStatusApproved},
		{"  Approved  ", template.TemplateStatusApproved},
		{"rejected", template.TemplateStatusRejected},
	}
	for _, tc := range cases {
		client := &createMockWAClient{createOutput: &conversation.CreateTemplateOutput{ID: "ext-1", Status: tc.raw}}
		factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
		repo := &capturingTemplateRepo{}
		uc := NewCreateTemplateUseCase(factory, repo, &fakeSetHeaderMediaUC{})

		out, err := uc.Execute(baseCreateInput(positionalBodyComponent()))
		if err != nil {
			t.Fatalf("raw=%q: unexpected error: %v", tc.raw, err)
		}
		if out.Status != tc.want {
			t.Errorf("raw=%q: output status = %q, want %q", tc.raw, out.Status, tc.want)
		}
		if repo.created == nil {
			t.Fatalf("raw=%q: expected template to be persisted", tc.raw)
		}
		if repo.created.Status != tc.want {
			t.Errorf("raw=%q: persisted status = %q, want %q", tc.raw, repo.created.Status, tc.want)
		}
	}
}
