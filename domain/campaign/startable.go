package campaign

import "errors"

var (
	ErrNothingToSend = errors.New("campaign start: the campaign has no numbers to send to")
	ErrAlreadySent   = errors.New("campaign start: every number has already been processed")
)

func (m *Metrics) Startable() error {
	if m == nil || m.TotalNumbers == 0 {
		return ErrNothingToSend
	}
	if m.Pending == 0 && m.Processed == m.TotalNumbers {
		return ErrAlreadySent
	}
	return nil
}
