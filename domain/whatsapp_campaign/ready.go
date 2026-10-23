package whatsapp_campaign

import "encoding/json"

func (c Campaign) ReadyToStart() bool {
	return c.Metrics.ReadyToStart(c.Status, !c.ScheduledStart.IsZero())
}

func (c Campaign) MarshalJSON() ([]byte, error) {
	type fields Campaign
	return json.Marshal(struct {
		fields
		ReadyToStart bool `json:"readyToStart"`
	}{fields(c), c.ReadyToStart()})
}
