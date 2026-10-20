package unofficial_whatsapp_campaign

import "time"

type SendEstimate struct {
	Duration   time.Duration
	Pending    int
	Senders    int
	Unsendable int
}

func PacingInterval(minMS, maxMS int) time.Duration {
	if maxMS < minMS {
		maxMS = minMS
	}
	return time.Duration(minMS+maxMS) * time.Millisecond / 2
}

func EstimateSendTime(candidates [][]string, intervals map[string]time.Duration) SendEstimate {
	lanes := map[string]time.Duration{}
	out := SendEstimate{}
	for _, senders := range candidates {
		chosen, finish := "", time.Duration(0)
		for _, id := range senders {
			interval, ok := intervals[id]
			if !ok || interval <= 0 {
				continue
			}
			next := lanes[id] + interval
			if chosen == "" || next < finish || (next == finish && id < chosen) {
				chosen, finish = id, next
			}
		}
		if chosen == "" {
			out.Unsendable++
			continue
		}
		lanes[chosen] = finish
		out.Pending++
	}
	for _, total := range lanes {
		if total > out.Duration {
			out.Duration = total
		}
	}
	out.Senders = len(lanes)
	return out
}
