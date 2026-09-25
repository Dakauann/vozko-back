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
	headerMediaUC template.SetTemplateHeaderMediaUseCase
	storage       StorageURLs
}

type StorageURLs interface {
	KeyFromURL(url string) (string, bool)
}

func NewCreateTemplateUseCase(clientFactory template.WhatsAppClientFactory, templateRepo template.Repository, headerMediaUC template.SetTemplateHeaderMediaUseCase, storage StorageURLs) template.CreateTemplateUseCase {
	return &createTemplateUseCase{
		clientFactory: clientFactory,
		templateRepo:  templateRepo,
		headerMediaUC: headerMediaUC,
		storage:       storage,
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

	domainComponents := make([]template.TemplateComponent, 0, len(input.Components))
	for _, c := range input.Components {
		comp := template.TemplateComponent{
			Type:                      strings.ToUpper(c.Type),
			Format:                    c.Format,
			Text:                      c.Text,
			AddSecurityRecommendation: c.AddSecurityRecommendation,
			CodeExpirationMinutes:     c.CodeExpirationMinutes,
		}

		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, template.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
				OTPType:     b.OTPType,
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

	if err := template.ValidateDraft(name, input.Category, domainComponents); err != nil {
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

	apiComponents := template.ToClientComponents(domainComponents)

	if err := uc.processHeaderMediaURLs(client, apiComponents); err != nil {
		return nil, err
	}

	effectiveFormat := (&template.Template{Components: domainComponents}).GetEffectiveParameterFormat()

	metaParameterFormat := effectiveFormat.ToMetaAPIFormat()
	if input.Category == template.TemplateCategoryAuthentication {
		metaParameterFormat = ""
	}

	apiOutput, err := client.CreateTemplate(context.Background(), conversation.CreateTemplateInput{
		Name:            strings.ToLower(name),
		Language:        language,
		Category:        string(input.Category),
		ParameterFormat: metaParameterFormat,
		Components:      apiComponents,
	})
	if err != nil {
		return nil, err
	}

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
			if uc.storage == nil {
				return template.ErrHeaderMediaOutsideStorage
			}
			if _, ours := uc.storage.KeyFromURL(handle); !ours {
				return template.ErrHeaderMediaOutsideStorage
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
