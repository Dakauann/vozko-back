package template_usecase

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

type createTemplateUseCase struct {
	clientFactory template.WhatsAppClientFactory
	templateRepo  template.Repository
	// headerMediaUC mints and links the WhatsApp media id for a media-header
	// template (URL -> /media upload -> id). Reused from the PATCH /header-media
	// endpoint so create doesn't duplicate the download/upload logic.
	headerMediaUC template.SetTemplateHeaderMediaUseCase
}

func NewCreateTemplateUseCase(clientFactory template.WhatsAppClientFactory, templateRepo template.Repository, headerMediaUC template.SetTemplateHeaderMediaUseCase) template.CreateTemplateUseCase {
	return &createTemplateUseCase{
		clientFactory: clientFactory,
		templateRepo:  templateRepo,
		headerMediaUC: headerMediaUC,
	}
}

func (uc *createTemplateUseCase) Execute(input template.CreateTemplateInput) (*template.CreateTemplateOutput, error) {
	if input.BusinessPhoneID == "" {
		return nil, errors.New("businessPhoneId is required")
	}

	wabaID, err := uc.clientFactory.WABAIdForPhone(input.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	client, err := uc.clientFactory.ClientForPhone(input.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(input.Name)

	if err := template.ValidateName(strings.ToLower(name)); err != nil {
		return nil, err
	}

	if !input.Category.IsValid() {
		return nil, template.ErrInvalidCategory
	}

	domainComponents := make([]template.TemplateComponent, 0, len(input.Components))
	for _, c := range input.Components {
		comp := template.TemplateComponent{
			Type:   strings.ToUpper(c.Type),
			Format: c.Format,
			Text:   c.Text,
		}

		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, template.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
			})
		}

		if c.Example != nil {
			comp.Example = &template.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}

			for _, np := range c.Example.BodyTextNamed {
				comp.Example.BodyTextNamed = append(comp.Example.BodyTextNamed, template.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}

			for _, np := range c.Example.HeaderTextNamed {
				comp.Example.HeaderTextNamed = append(comp.Example.HeaderTextNamed, template.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
		domainComponents = append(domainComponents, comp)
	}

	if err := template.ValidateComponents(domainComponents); err != nil {
		return nil, err
	}

	hasMediaHeader := false
	for _, c := range domainComponents {
		format := strings.ToUpper(c.Format)
		if strings.ToUpper(c.Type) == "HEADER" && (format == "IMAGE" || format == "VIDEO" || format == "DOCUMENT") {
			hasMediaHeader = true
			break
		}
	}

	headerMediaURLProvided := input.HeaderMediaURL != nil && strings.TrimSpace(*input.HeaderMediaURL) != ""
	if headerMediaURLProvided && !hasMediaHeader {
		return nil, template.ErrHeaderMediaURLNotApplicable
	}

	if hasMediaHeader && !headerMediaURLProvided {
		log.Printf("[template-create] WARNING: Template %s has media header but no headerMediaUrl provided. You can set it later via PATCH /whatsapp/templates/{id}/header-media", name)
	}

	language := input.Language
	if language == "" {
		language = "pt_BR"
	}

	apiComponents := make([]conversation.TemplateComponent, 0, len(domainComponents))
	for _, c := range domainComponents {
		comp := conversation.TemplateComponent{
			Type:   c.Type,
			Format: c.Format,
			Text:   c.Text,
		}

		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, conversation.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
			})
		}

		if c.Example != nil {
			comp.Example = &conversation.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}

			for _, np := range c.Example.BodyTextNamed {
				comp.Example.BodyTextNamed = append(comp.Example.BodyTextNamed, conversation.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}

			for _, np := range c.Example.HeaderTextNamed {
				comp.Example.HeaderTextNamed = append(comp.Example.HeaderTextNamed, conversation.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
		apiComponents = append(apiComponents, comp)
	}

	if err := uc.processHeaderMediaURLs(client, apiComponents); err != nil {
		return nil, err
	}

	// Meta's Cloud API requires the top-level parameter_format to be present and
	// to match the placeholder style in the body. Named placeholders ({{order}})
	// sent without parameter_format=NAMED are rejected with INVALID_FORMAT. The
	// body is the source of truth, so we infer the format from the actual
	// components here instead of trusting the (historically empty) client value.
	effectiveFormat := (&template.Template{Components: domainComponents}).GetEffectiveParameterFormat()

	apiOutput, err := client.CreateTemplate(context.Background(), conversation.CreateTemplateInput{
		Name:            strings.ToLower(name),
		Language:        language,
		Category:        string(input.Category),
		ParameterFormat: effectiveFormat.ToMetaAPIFormat(),
		Components:      apiComponents,
	})
	if err != nil {
		return nil, err
	}

	// 360dialog's channel-scoped create endpoint returns the status lowercased
	// ("pending"/"approved"/"rejected"), whereas Meta returns it uppercased and the
	// domain (IsApproved/CanSend) compares against the uppercase TemplateStatus
	// constants. Without normalising, a 360dialog template is persisted as
	// "pending" and never satisfies IsApproved — so it can never be sent even after
	// Meta approves it. Mirror the sync path, which already uppercases.
	status := template.TemplateStatus(strings.ToUpper(strings.TrimSpace(apiOutput.Status)))

	if status == template.TemplateStatusRejected {
		log.Printf("[template-create] Template %s REJECTED by Meta (rejected_reason=%s, parameter_format=%s)", name, apiOutput.RejectedReason, effectiveFormat.ToMetaAPIFormat())
	}

	tmpl := &template.Template{
		ID:              uuid.New().String(),
		ExternalID:      apiOutput.ID,
		WABAId:          wabaID,
		Name:            strings.ToLower(name),
		Language:        language,
		Category:        input.Category,
		Status:          status,
		ParameterFormat: effectiveFormat,
		Components:      domainComponents,
		HeaderMediaURL:  input.HeaderMediaURL,
	}

	if err := uc.templateRepo.Create(tmpl); err != nil {
		log.Printf("[template-create] WARNING: failed to persist template %s locally: %v", name, err)
	}

	// Standardize sending on a WhatsApp media id. Every campaign/workflow/tool send
	// path attaches the header media by id (header_handle is only the create-time
	// example), so a media-header template needs its id minted up front — otherwise
	// the first send goes out without its required header and is rejected. Reuse the
	// PATCH /header-media use case (download URL -> /media upload -> id, linked to the
	// template) instead of duplicating that logic here. Best-effort: the template
	// already exists at the provider and is persisted with the URL, so a failure is
	// logged and left to be retried via PATCH rather than failing the whole create.
	if hasMediaHeader && headerMediaURLProvided && uc.headerMediaUC != nil {
		if err := uc.headerMediaUC.Execute(template.SetTemplateHeaderMediaInput{
			TemplateID:     tmpl.ID,
			HeaderMediaURL: input.HeaderMediaURL,
		}); err != nil {
			log.Printf("[template-create] WARNING: failed to mint header media id for template %s: %v. Sends will fail until it is set via PATCH /whatsapp/templates/{id}/header-media", name, err)
		}
	}

	return &template.CreateTemplateOutput{
		ID:             tmpl.ID,
		ExternalID:     apiOutput.ID,
		Name:           tmpl.Name,
		Status:         status,
		RejectedReason: apiOutput.RejectedReason,
	}, nil
}

func (uc *createTemplateUseCase) processHeaderMediaURLs(client conversation.WhatsAppClient, components []conversation.TemplateComponent) error {
	// 360dialog's channel-scoped template endpoint wants the public media URL
	// verbatim in header_handle and fetches it itself; uploading the URL to a
	// Resumable-Upload handle first and sending that handle makes it reject the
	// payload with 400 "it should be valid url address". So for those channels we
	// leave the URL in place instead of uploading. Meta (no capability / false)
	// still needs the upload-to-handle step below.
	if mc, ok := client.(conversation.WhatsAppTemplateMediaClient); ok && mc.TemplateHeaderMediaWantsURL() {
		return nil
	}

	for i := range components {
		comp := &components[i]

		if strings.ToUpper(comp.Type) != "HEADER" {
			continue
		}

		format := strings.ToUpper(comp.Format)
		if format != "IMAGE" && format != "VIDEO" && format != "DOCUMENT" {
			continue
		}

		if comp.Example == nil || len(comp.Example.HeaderHandle) == 0 {
			continue
		}

		for j, handle := range comp.Example.HeaderHandle {
			if !isURL(handle) {
				continue
			}

			log.Printf("[template-create] Detected URL in header_handle: %s, uploading to Meta...", handle)

			fileName := inferFileNameFromURL(handle)

			uploadedHandle, err := client.UploadMediaForTemplate(context.Background(), conversation.UploadMediaForTemplateInput{
				URL:      handle,
				FileName: fileName,
			})
			if err != nil {
				return err
			}

			log.Printf("[template-create] Uploaded URL to Meta, got handle: %s", uploadedHandle)

			comp.Example.HeaderHandle[j] = uploadedHandle
		}
	}

	return nil
}

func isURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func inferFileNameFromURL(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		if idx := strings.Index(lastPart, "?"); idx > 0 {
			lastPart = lastPart[:idx]
		}
		if lastPart != "" && strings.Contains(lastPart, ".") {
			return lastPart
		}
	}
	return "media_file"
}
