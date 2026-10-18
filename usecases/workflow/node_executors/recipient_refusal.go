package node_executors

import "errors"

func failsTheStep(sendErr error) bool {
	return errors.Is(sendErr, errRecipientRefused)
}
