package marketing

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const pageNumberVerificationEdge = "/page_whatsapp_number_verification"

type graphPageNumberVerification struct {
	Status       string `json:"verification_status"`
	ErrorMessage string `json:"error_message"`
}

func (g *Gateway) RequestPageNumberCode(ctx context.Context, token, pageID, number string) (string, error) {
	return g.verifyPageNumber(ctx, token, pageID, url.Values{"whatsapp_number": {number}})
}

func (g *Gateway) VerifyPageNumber(ctx context.Context, token, pageID, number, code string) (string, error) {
	return g.verifyPageNumber(ctx, token, pageID, url.Values{"whatsapp_number": {number}, "verification_code": {code}})
}

func (g *Gateway) verifyPageNumber(ctx context.Context, token, pageID string, form url.Values) (string, error) {
	path, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return "", err
	}
	var out graphPageNumberVerification
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: path + pageNumberVerificationEdge, Token: pageToken, Form: form}, &out); err != nil {
		return "", err
	}
	if message := strings.TrimSpace(out.ErrorMessage); message != "" {
		return "", &advertising.PageLinkRefusal{Answer: message}
	}
	return out.Status, nil
}
