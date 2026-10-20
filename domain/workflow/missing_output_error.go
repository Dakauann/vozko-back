package workflow

import (
	"errors"
	"fmt"
)

type MissingOutputError struct {
	NodeID   string
	OutputID string
}

func (e *MissingOutputError) Error() string {
	return fmt.Sprintf("%v: node %q output %q", ErrNodeMissingRequiredOutput, e.NodeID, e.OutputID)
}

func (e *MissingOutputError) Unwrap() error {
	return ErrNodeMissingRequiredOutput
}

func missingOutputOf(err error) string {
	var missing *MissingOutputError
	if errors.As(err, &missing) {
		return missing.OutputID
	}
	return ""
}
