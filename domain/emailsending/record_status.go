package emailsending

import "strings"

type RecordStatus string

const (
	RecordVerified RecordStatus = "verified"
	RecordPending  RecordStatus = "pending"
	RecordUnknown  RecordStatus = "unknown"
)

func (s RecordStatus) Valid() bool {
	switch s {
	case RecordVerified, RecordPending, RecordUnknown:
		return true
	}
	return false
}

func (r DNSRecord) CurrentStatus() RecordStatus {
	if r.Status.Valid() {
		return r.Status
	}
	return RecordPending
}

type DNSAnswer struct {
	Values []string
	Failed bool
}

type DNSAnswers struct {
	MX  map[string]DNSAnswer
	TXT map[string]DNSAnswer
}

func RecordLookups(records []DNSRecord) (mx, txt []string) {
	for _, r := range records {
		switch r.Purpose {
		case RecordReplyMX:
			mx = append(mx, hostKey(r.Name))
		case RecordDMARC:
			txt = append(txt, hostKey(r.Name))
		}
	}
	return mx, txt
}

func CheckRecords(records []DNSRecord, state IdentityState, answers DNSAnswers) []DNSRecord {
	out := make([]DNSRecord, len(records))
	for i, r := range records {
		r.Status = recordStatus(r, state, answers)
		out[i] = r
	}
	return out
}

func recordStatus(r DNSRecord, state IdentityState, answers DNSAnswers) RecordStatus {
	switch r.Purpose {
	case RecordDKIM:
		return verifiedWhen(state.DKIMVerified)
	case RecordMailFromMX, RecordMailFromSPF:
		return verifiedWhen(state.MailFromVerified)
	case RecordReplyMX:
		want := hostKey(r.Value)
		return observed(answers.MX, r.Name, func(v string) bool { return hostKey(v) == want })
	case RecordDMARC:
		return observed(answers.TXT, r.Name, DMARCCompatible)
	}
	return RecordUnknown
}

func verifiedWhen(ok bool) RecordStatus {
	if ok {
		return RecordVerified
	}
	return RecordPending
}

func observed(answers map[string]DNSAnswer, name string, matches func(string) bool) RecordStatus {
	answer, ok := answers[hostKey(name)]
	if !ok || answer.Failed {
		return RecordUnknown
	}
	for _, v := range answer.Values {
		if matches(v) {
			return RecordVerified
		}
	}
	return RecordPending
}

func hostKey(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}
