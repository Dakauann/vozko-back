package tiktok

import (
	"errors"
	"fmt"
	"testing"

	ttdomain "vozko/domain/tiktok"
	webhook_usecase "vozko/usecases/webhook"
)

func TestConsumerClassification(t *testing.T) {
	c := Classifier{Errors: fakeClassifier{}, Metrics: &fakeMetrics{}}
	cases := []struct {
		name string
		err  error
		want webhook_usecase.Disposition
	}{
		{"unknown account", fmt.Errorf("x: %w", ErrUnknownAccount), webhook_usecase.DispositionDrop},
		{"account without the capability", ErrAccountUnavailable, webhook_usecase.DispositionDrop},
		{"unknown event", ttdomain.ErrUnknownEvent, webhook_usecase.DispositionDrop},
		{"malformed content", fmt.Errorf("%w: bad", ttdomain.ErrInvalidContent), webhook_usecase.DispositionDrop},
		{"lost authorization", ttdomain.ErrNeedsReconnect, webhook_usecase.DispositionDrop},
		{"auth refused", codedError{auth: true}, webhook_usecase.DispositionDrop},
		{"tiktok refused the parameters", codedError{invalid: true}, webhook_usecase.DispositionDeadLetter},
		{"rate limited", codedError{rate: true, retry: true}, webhook_usecase.DispositionRetry},
		{"database down", errors.New("connection refused"), webhook_usecase.DispositionRetry},
	}
	for _, tc := range cases {
		if got := c.Disposition(tc.err); got != tc.want {
			t.Errorf("%s: disposition %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDedupKeyIsTheEventIdempotencyKeyAndEmptyForInvalidEnvelopes(t *testing.T) {
	env := ttdomain.Envelope{Event: "im_receive_msg", UserOpenID: "_000abc", CreateTime: 1, Content: `{"conversation_id":"c","message_id":"m1","unique_identifier":"p","timestamp":1,"type":"text","text":{"body":"oi"}}`}
	if got := DedupKey(&env); got != "tt:msg:in:m1" {
		t.Fatalf("key = %q", got)
	}
	broken := env
	broken.Content = "{"
	if got := DedupKey(&broken); got != "" {
		t.Fatalf("broken key = %q", got)
	}
	if DedupKey(nil) != "" {
		t.Fatal("nil envelope")
	}
}
