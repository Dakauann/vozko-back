package notification_service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailapi"
	"vozko/domain/notification"
	emailapi_usecase "vozko/usecases/emailapi"
)

const systemEmailTimeout = 30 * time.Second

var ErrSystemEmailIncomplete = errors.New("system email: the workspace, the sender, the template loader and the email API are required")

type SystemEmailConfig struct {
	WorkspaceID string
	SenderID    string
}

type SystemEmailService struct {
	sender emailapi_usecase.Sender
	loader notification.TemplateLoader
	cfg    SystemEmailConfig
	newKey func() string
}

var _ notification.EmailService = (*SystemEmailService)(nil)

func NewSystemEmailService(sender emailapi_usecase.Sender, loader notification.TemplateLoader, cfg SystemEmailConfig) (*SystemEmailService, error) {
	if sender == nil || loader == nil || strings.TrimSpace(cfg.WorkspaceID) == "" || strings.TrimSpace(cfg.SenderID) == "" {
		return nil, ErrSystemEmailIncomplete
	}
	return &SystemEmailService{sender: sender, loader: loader, cfg: cfg, newKey: uuid.NewString}, nil
}

func (s *SystemEmailService) SendEmail(to, subject, body string) error {
	var recipients []string
	for _, part := range strings.Split(to, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), systemEmailTimeout)
	defer cancel()
	_, err := s.sender.Send(ctx, emailapi.Request{
		WorkspaceID:    s.cfg.WorkspaceID,
		Origin:         emailapi.OriginSystem,
		From:           s.cfg.SenderID,
		To:             recipients,
		Content:        emailapi.Content{Subject: subject, HTML: body},
		Tags:           []emailapi.Tag{{Name: "origin", Value: "system"}},
		IdempotencyKey: "system:" + s.newKey(),
	})
	if err != nil {
		return fmt.Errorf("system email to %d recipients: %w", len(recipients), err)
	}
	return nil
}

func (s *SystemEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	body, err := s.loader.LoadTemplate(templateName, data)
	if err != nil {
		return fmt.Errorf("system email template %s: %w", templateName, err)
	}
	return s.SendEmail(to, subject, body)
}
