package notification

type EmailService interface {
	SendEmail(to, subject, body string) error
	SendTemplate(to, subject, templateName string, data map[string]interface{}) error
}

type TemplateLoader interface {
	LoadTemplate(filePath string, data map[string]interface{}) (string, error)
}
