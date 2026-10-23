package emailsending

const SpamRateAlertPerMil = 3

func (s DomainStats) Alarming() bool {
	return s.Available && s.SpamRatePerMil >= SpamRateAlertPerMil
}
