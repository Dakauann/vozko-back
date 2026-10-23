package emailinbox

import (
	"sort"

	"vozko/domain/emailsending"
)

func PickSender(senders []emailsending.Sender, domainID string) (emailsending.Sender, bool) {
	if len(senders) == 0 {
		return emailsending.Sender{}, false
	}
	ordered := append([]emailsending.Sender(nil), senders...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	ranks := []func(emailsending.Sender) bool{
		func(s emailsending.Sender) bool { return s.DomainID == domainID && s.IsDefault },
		func(s emailsending.Sender) bool { return s.DomainID == domainID },
		func(s emailsending.Sender) bool { return s.IsDefault },
		func(emailsending.Sender) bool { return true },
	}
	for _, matches := range ranks {
		for _, s := range ordered {
			if matches(s) {
				return s, true
			}
		}
	}
	return emailsending.Sender{}, false
}
