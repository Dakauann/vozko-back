package workflow

import (
	"math"
	"strings"
	"time"
)

const EntryTypeLead = "lead"

const (
	DataKeyLeadID   = "lead_id"
	DataKeyLeadName = "lead_name"
	DataKeyBirthday = "birthday"
)

const (
	BirthdayHourKey     = "hour"
	DefaultBirthdayHour = 9
	BirthdaySweepPeriod = 15 * time.Minute
	birthdayDateLayout  = "2006-01-02"
)

func (e TriggerEvent) StartsOnLead() bool {
	if e.EntryType != EntryTypeLead {
		return false
	}
	return e.TriggerType == TriggerBirthday || e.TriggerType == TriggerWebhook
}

func (r *WorkflowRun) OnLead() bool {
	return r.EntryType == EntryTypeLead
}

func TriggerNodeStartsOnLead(n Node) bool {
	switch n.Type {
	case NodeTypeTriggerBirthday:
		return true
	case NodeTypeTriggerWebhook:
		start, ok := WebhookStartOf(n.Config)
		return ok && start == WebhookStartLead
	}
	return false
}

func BirthdayDue(config map[string]interface{}, localNow time.Time) bool {
	hour, ok := birthdayHourOf(config)
	return ok && localNow.Hour() >= hour
}

func birthdayHourOf(config map[string]interface{}) (int, bool) {
	raw, present := config[BirthdayHourKey]
	if !present || raw == nil {
		return DefaultBirthdayHour, true
	}
	value, ok := numericConfigValue(raw)
	if !ok || value != math.Trunc(value) || value < 0 || value > 23 {
		return 0, false
	}
	return int(value), true
}

type LeadSubject struct {
	WorkspaceID string
	LeadID      string
	LeadName    string
}

func (s LeadSubject) event(trigger TriggerType, data map[string]interface{}) TriggerEvent {
	return TriggerEvent{
		WorkspaceID: s.WorkspaceID,
		EntryID:     s.LeadID,
		EntryType:   EntryTypeLead,
		TriggerType: trigger,
		Data:        s.RunData(data),
	}
}

func BirthdayEvent(subject LeadSubject, localDay time.Time) TriggerEvent {
	return subject.event(TriggerBirthday, BirthdayRunData(localDay))
}

func leadOccurrenceKey(e TriggerEvent) string {
	switch e.TriggerType {
	case TriggerBirthday:
		day, _ := e.Data[DataKeyBirthday].(string)
		lead, day := strings.TrimSpace(e.EntryID), strings.TrimSpace(day)
		if lead == "" || day == "" {
			return ""
		}
		return "birthday/" + lead + "/" + day
	case TriggerWebhook:
		occurrence := strings.TrimSpace(e.Occurrence)
		if occurrence == "" {
			return ""
		}
		return "webhook/" + occurrence
	}
	return ""
}
