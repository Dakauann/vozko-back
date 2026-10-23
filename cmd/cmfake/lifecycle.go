package main

import (
	"strings"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/infra/cmcom"
	"vozko/infra/cmcom/cmfake"
)

const (
	channelRCS             = "rcs"
	channelSMS             = "sms"
	acceptedAfter          = 500 * time.Millisecond
	deliveredAfter         = 2 * time.Second
	readAfter              = 5 * time.Second
	unreachableDescription = cmfake.UnreachableDetail
	rejectedCode           = 403
	rejectedDetail         = "Message rejected"
)

type step struct {
	After       time.Duration
	Status      carriermessaging.Status
	ErrorCode   string
	Description string
}

func lifecycle(channel, phone string) []step {
	if strings.HasSuffix(phone, "2") {
		return nil
	}
	steps := []step{{After: acceptedAfter, Status: carriermessaging.StatusAccepted, Description: "Accepted"}}
	if channel == channelRCS && strings.HasSuffix(phone, "1") {
		return append(steps, step{After: deliveredAfter, Status: carriermessaging.StatusFailed, ErrorCode: cmcom.UnreachableErrorCode, Description: unreachableDescription})
	}
	steps = append(steps, step{After: deliveredAfter, Status: carriermessaging.StatusDelivered, Description: "Delivered"})
	if channel == channelRCS {
		steps = append(steps, step{After: readAfter, Status: carriermessaging.StatusRead, Description: "Read"})
	}
	return steps
}

func outcomeFor(phone string) cmfake.Outcome {
	if strings.HasSuffix(phone, "3") {
		return cmfake.Reject(rejectedCode, rejectedDetail)
	}
	return cmfake.Accept()
}
