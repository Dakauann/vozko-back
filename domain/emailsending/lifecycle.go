package emailsending

type DomainLifecycle string

const (
	LifecycleActive    DomainLifecycle = "active"
	LifecycleReleasing DomainLifecycle = "releasing"
	LifecycleRetired   DomainLifecycle = "retired"
)

func (d SendingDomain) Lifecycle() DomainLifecycle {
	switch {
	case d.AwaitsRelease():
		return LifecycleReleasing
	case d.Retired:
		return LifecycleRetired
	}
	return LifecycleActive
}

func (d SendingDomain) Listed() bool {
	return d.Lifecycle() != LifecycleRetired
}

type DomainActions struct {
	Recheck bool
	Remove  bool
}

func (d SendingDomain) Actions() DomainActions {
	if d.Lifecycle() != LifecycleActive {
		return DomainActions{}
	}
	return DomainActions{Recheck: true, Remove: d.Kind == DomainOwn}
}
