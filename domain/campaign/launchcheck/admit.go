package launchcheck

import "errors"

var (
	ErrBlocked     = errors.New("the campaign cannot start until its blocks are solved")
	ErrUnconfirmed = errors.New("the campaign's risks need the person's confirmation")
)

type RefusedError struct {
	Report Report
}

func (e *RefusedError) Error() string {
	return e.Unwrap().Error()
}

func (e *RefusedError) Unwrap() error {
	if e.Report.Blocks() {
		return ErrBlocked
	}
	return ErrUnconfirmed
}

func (r Report) Admit(confirmedFingerprint string) error {
	if r.Blocks() || !r.Confirmed(confirmedFingerprint) {
		return &RefusedError{Report: r}
	}
	return nil
}
