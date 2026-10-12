package openroutergen

import (
	"fmt"
	"io"
	"net/http"

	"vozko/domain/mediagen"
	"vozko/infra/ai/openrouterlimit"
)

const maxRefusalBody = 64 << 10

func refusal(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxRefusalBody))
	return refused(resp, raw)
}

func refused(resp *http.Response, raw []byte) error {
	failed := fmt.Errorf("%w: status %d: %s", mediagen.ErrGenerationFailed, resp.StatusCode, truncate(raw))
	if busy := openrouterlimit.RefusalInBody(raw, resp.Header, failed); busy != nil {
		return fmt.Errorf("%w: %w", mediagen.ErrGenerationFailed, busy)
	}
	return failed
}
