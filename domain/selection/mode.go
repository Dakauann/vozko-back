package selection

import "vozko/domain/crmfilter"

func ModeFor(picked bool, filter *crmfilter.Filter) Mode {
	switch {
	case picked:
		return ModeIDs
	case filter != nil && !filter.IsEmpty():
		return ModeAllMatching
	case filter != nil:
		return ModeEveryone
	default:
		return ""
	}
}

func (s Selection) Inferred() Selection {
	if s.Mode == "" {
		s.Mode = ModeFor(len(s.IDs) > 0, s.Filter)
	}
	return s
}

func ForFilter(filter crmfilter.Filter) Selection {
	counted := Selection{Mode: ModeFor(false, &filter), Filter: &filter}
	counted.Fingerprint = Fingerprint(counted.EffectiveFilter())
	return counted
}

func (s Selection) BeforeExclusions() Selection {
	s.ExcludeIDs = nil
	return s
}
