package carriermessaging

type SuppressionSource string

func AllStatuses() []Status {
	return []Status{StatusPending, StatusAccepted, StatusDelivered, StatusRead, StatusRejected, StatusFailed, StatusExpired}
}

func (s Step) Valid() bool {
	return s == StepRCS || s == StepWhatsApp || s == StepSMS
}

func (k RunKind) Valid() bool {
	switch k {
	case RunCampaign, RunAutomation, RunOperator, RunWorkflow, RunTest:
		return true
	}
	return false
}

func (k EventKind) Valid() bool {
	switch k {
	case EventInbound, EventTap, EventClick, EventOptOut:
		return true
	}
	return false
}

func (s SuppressionSource) Valid() bool {
	return s == SuppressionKeyword || s == SuppressionButton || s == SuppressionOperator
}
