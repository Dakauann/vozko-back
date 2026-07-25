package scriptvm

import "time"

type Limits struct {
	Wallclock time.Duration

	MaxOutputBytes int

	MaxLogBytes int

	MaxFetchCalls int

	MaxFetchBodyBytes int

	AllowFetch bool

	AllowHTTPInsecure bool

	EgressAllowlist []string
}

func DefaultLimits() Limits {
	return Limits{
		Wallclock:         5 * time.Second,
		MaxOutputBytes:    256 * 1024,
		MaxLogBytes:       64 * 1024,
		MaxFetchCalls:     20,
		MaxFetchBodyBytes: 5 * 1024 * 1024,
		AllowFetch:        true,
		AllowHTTPInsecure: false,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.Wallclock <= 0 {
		l.Wallclock = d.Wallclock
	}
	if l.MaxOutputBytes <= 0 {
		l.MaxOutputBytes = d.MaxOutputBytes
	}
	if l.MaxLogBytes <= 0 {
		l.MaxLogBytes = d.MaxLogBytes
	}
	if l.MaxFetchCalls < 0 {
		l.MaxFetchCalls = d.MaxFetchCalls
	}
	if l.MaxFetchBodyBytes <= 0 {
		l.MaxFetchBodyBytes = d.MaxFetchBodyBytes
	}
	return l
}
