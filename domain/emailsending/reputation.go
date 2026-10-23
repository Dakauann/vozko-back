package emailsending

const (
	BounceWarningPerMil    = 20
	BounceLimitPerMil      = 50
	ComplaintWarningPerMil = 1
	ComplaintLimitPerMil   = 3
	basisPointsPerMil      = 10
)

type ReputationLevel string

const (
	ReputationOK      ReputationLevel = "ok"
	ReputationWarning ReputationLevel = "warning"
	ReputationOver    ReputationLevel = "over"
)

type ReputationBand struct {
	WarningBasisPoints int
	LimitBasisPoints   int
}

func BounceBand() ReputationBand {
	return ReputationBand{WarningBasisPoints: BounceWarningPerMil * basisPointsPerMil, LimitBasisPoints: BounceLimitPerMil * basisPointsPerMil}
}

func ComplaintBand() ReputationBand {
	return ReputationBand{WarningBasisPoints: ComplaintWarningPerMil * basisPointsPerMil, LimitBasisPoints: ComplaintLimitPerMil * basisPointsPerMil}
}

func (b ReputationBand) Level(rateBasisPoints int) ReputationLevel {
	switch {
	case rateBasisPoints >= b.LimitBasisPoints:
		return ReputationOver
	case rateBasisPoints >= b.WarningBasisPoints:
		return ReputationWarning
	}
	return ReputationOK
}
