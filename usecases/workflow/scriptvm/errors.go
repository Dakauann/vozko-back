package scriptvm

import "errors"

var (
	ErrTimeout = errors.New("scriptvm: execution timeout")

	ErrDisabled = errors.New("scriptvm: scripting is disabled")

	ErrOutputTooLarge = errors.New("scriptvm: output exceeds size limit")

	ErrStaticReject = errors.New("scriptvm: code rejected by static analyzer")
)

type ScriptError struct {
	Message string
	Stack   string
}

func (e *ScriptError) Error() string { return e.Message }
