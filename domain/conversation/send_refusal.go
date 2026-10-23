package conversation

import "errors"

type SendRefusal struct {
	Code string
	Err  error
}

func (r *SendRefusal) Error() string {
	if r.Err == nil {
		return r.Code
	}
	return r.Err.Error()
}

func (r *SendRefusal) Unwrap() error { return r.Err }

func SendFailureCode(err error, fallback string) string {
	var refusal *SendRefusal
	if errors.As(err, &refusal) && refusal.Code != "" {
		return refusal.Code
	}
	return fallback
}
