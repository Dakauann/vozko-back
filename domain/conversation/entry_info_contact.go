package conversation

import "strings"

const UnknownContactLabel = "um contato"

func (i EntryInfo) ContactLabel() string {
	if name := strings.TrimSpace(i.LeadName); name != "" {
		return name
	}
	if number := strings.TrimSpace(i.LeadNumber); number != "" {
		return number
	}
	return UnknownContactLabel
}
