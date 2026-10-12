package openrouter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
	"vozko/infra/ai/openrouterlimit"
)

func classified(err error) error {
	if busy := atSpendingLimit(err); busy != nil {
		return busy
	}
	if err == nil || errors.Is(err, ai.ErrProviderTransient) || !passingFault(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ai.ErrProviderTransient, err)
}

func atSpendingLimit(err error) error {
	var apiErr *openrouter.APIError
	if !errors.As(err, &apiErr) || apiErr.Metadata == nil || errors.Is(err, ai.ErrProviderBusy) {
		return nil
	}
	return openrouterlimit.Refusal(*apiErr.Metadata, nil, err)
}

func passingFault(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, ai.ErrStreamIncomplete) {
		return true
	}
	if status, ok := openrouter.HTTPStatusCode(err); ok {
		return passingStatus(status)
	}
	if code, ok := openrouter.APIErrorCode(err); ok {
		status, numeric := code.(int)
		return numeric && passingStatus(status)
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE)
}

func passingStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return status >= 520 && status <= 529
}
