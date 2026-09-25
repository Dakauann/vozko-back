package campaign

type Milestone string

const (
	MilestoneSent      Milestone = "sent"
	MilestoneDelivered Milestone = "delivered"
	MilestoneRead      Milestone = "read"
	MilestoneFailed    Milestone = "failed"
)

func AllMilestones() []Milestone {
	return []Milestone{MilestoneSent, MilestoneDelivered, MilestoneRead, MilestoneFailed}
}

func (s SendStatus) Milestones() []Milestone {
	switch s {
	case SendStatusSent:
		return []Milestone{MilestoneSent}
	case SendStatusDelivered:
		return []Milestone{MilestoneSent, MilestoneDelivered}
	case SendStatusRead:
		return []Milestone{MilestoneSent, MilestoneDelivered, MilestoneRead}
	case SendStatusFailed:
		return []Milestone{MilestoneFailed}
	default:
		return nil
	}
}
