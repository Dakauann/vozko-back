package metaplatform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	mp "vozko/domain/metaplatform"
)

type Service struct {
	requests  mp.DeletionRequestRepository
	handlers  map[mp.App][]mp.AppUserHandler
	statusURL string
	now       func() time.Time
}

type DeletionOutput struct {
	Code      string
	StatusURL string
}

func NewService(requests mp.DeletionRequestRepository, frontendBaseURL string) *Service {
	return &Service{
		requests:  requests,
		handlers:  make(map[mp.App][]mp.AppUserHandler),
		statusURL: strings.TrimRight(frontendBaseURL, "/") + "/data-deletion?code=",
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Register(app mp.App, handler mp.AppUserHandler) {
	if handler == nil {
		return
	}
	s.handlers[app] = append(s.handlers[app], handler)
}

func (s *Service) Deauthorize(ctx context.Context, app mp.App, appScopedUserID string) error {
	if err := validate(app, appScopedUserID); err != nil {
		return err
	}
	var errs []error
	for _, h := range s.handlers[app] {
		if err := h.RevokeAppUser(ctx, appScopedUserID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) RequestDeletion(ctx context.Context, app mp.App, appScopedUserID string) (*DeletionOutput, error) {
	if err := validate(app, appScopedUserID); err != nil {
		return nil, err
	}
	code, err := newCode()
	if err != nil {
		return nil, err
	}
	if err := s.requests.Create(ctx, &mp.DeletionRequest{
		Code:            code,
		App:             app,
		AppScopedUserID: appScopedUserID,
		Status:          mp.DeletionReceived,
		RequestedAt:     s.now(),
	}); err != nil {
		return nil, err
	}

	var errs []error
	for _, h := range s.handlers[app] {
		if err := h.EraseAppUser(ctx, appScopedUserID); err != nil {
			errs = append(errs, err)
		}
	}
	status, detail := mp.DeletionCompleted, ""
	if joined := errors.Join(errs...); joined != nil {
		status, detail = mp.DeletionFailed, joined.Error()
		log.Printf("[meta-platform] data deletion %s for %s user %s failed: %v", code, app, appScopedUserID, joined)
	}
	if err := s.requests.Finish(ctx, code, status, detail, s.now()); err != nil {
		return nil, err
	}
	return &DeletionOutput{Code: code, StatusURL: s.statusURL + code}, nil
}

func (s *Service) Status(ctx context.Context, code string) (*mp.DeletionRequest, error) {
	return s.requests.FindByCode(ctx, strings.TrimSpace(code))
}

func validate(app mp.App, appScopedUserID string) error {
	if !app.Valid() {
		return fmt.Errorf("%w: %q", mp.ErrUnknownApp, app)
	}
	if strings.TrimSpace(appScopedUserID) == "" {
		return mp.ErrAppScopedUserIDRequired
	}
	return nil
}

func newCode() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("meta platform: mint confirmation code: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
