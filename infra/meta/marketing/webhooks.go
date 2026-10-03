package marketing

import (
	"context"
	"net/http"
	"net/url"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var _ advertising.WebhookGateway = (*Gateway)(nil)

func (g *Gateway) SubscribeAccount(ctx context.Context, token, metaAccountID string) error {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return err
	}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: path + "/subscribed_apps", Token: token, Form: url.Values{}, Idempotent: true})
}
