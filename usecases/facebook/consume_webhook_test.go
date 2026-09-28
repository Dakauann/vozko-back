package facebook

import (
	"errors"
	"fmt"
	"testing"

	mm "vozko/domain/metamessaging"
	"vozko/infra/meta"
	webhook_usecase "vozko/usecases/webhook"
)

func TestWebhookFailureClassification(t *testing.T) {
	cases := []struct {
		err  error
		want webhook_usecase.Disposition
	}{
		{fmt.Errorf("x: %w", ErrUnknownPage), webhook_usecase.DispositionDrop},
		{mm.ErrInvalidWebhookPayload, webhook_usecase.DispositionDrop},
		{&meta.Error{Code: meta.CodeAccessTokenError}, webhook_usecase.DispositionDrop},
		{&meta.Error{Code: meta.CodePageRateLimit}, webhook_usecase.DispositionRetry},
		{errors.New("db down"), webhook_usecase.DispositionRetry},
		{&meta.Error{Code: meta.CodeInvalidParam}, webhook_usecase.DispositionDeadLetter},
	}
	for _, tc := range cases {
		if got := ClassifyWebhookFailure(tc.err); got != tc.want {
			t.Errorf("%v -> %v, want %v", tc.err, got, tc.want)
		}
	}
}
