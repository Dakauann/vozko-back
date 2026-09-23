package attendance

import "sort"

const (
	ClusterExcellence  = "excellence"
	ClusterOnTrack     = "on_track"
	ClusterImprovement = "improvement"
	ClusterCritical    = "critical"
)

const ReasonNoTargetsSet = "no_targets_set"

type ClusterBand struct {
	MinOnTrackPct float64 `json:"min_on_track_pct"`
	Cluster       string  `json:"cluster"`
}

type ClusterBands []ClusterBand

func DefaultClusterBands() ClusterBands {
	return ClusterBands{
		{MinOnTrackPct: 90, Cluster: ClusterExcellence},
		{MinOnTrackPct: 70, Cluster: ClusterOnTrack},
		{MinOnTrackPct: 0, Cluster: ClusterImprovement},
	}
}

func (b ClusterBands) clusterFor(onTrack int, onTrackPct float64) string {
	if onTrack == 0 {
		return ClusterCritical
	}
	bands := make(ClusterBands, len(b))
	copy(bands, b)
	sort.Slice(bands, func(i, j int) bool { return bands[i].MinOnTrackPct > bands[j].MinOnTrackPct })
	for _, band := range bands {
		if onTrackPct >= band.MinOnTrackPct {
			return band.Cluster
		}
	}
	return ClusterImprovement
}

type Standing struct {
	TargetsSet int     `json:"targets_set"`
	OnTrack    int     `json:"on_track"`
	AtRisk     int     `json:"at_risk"`
	OffTrack   int     `json:"off_track"`
	OnTrackPct float64 `json:"on_track_pct"`
	Cluster    string  `json:"cluster,omitempty"`
	Available  bool    `json:"available"`
	Reason     string  `json:"reason,omitempty"`
}

func BuildStanding(projections []MetricProjection, bands ClusterBands) Standing {
	out := Standing{}
	for _, p := range projections {
		if p.Target == nil {
			continue
		}
		out.TargetsSet++
		switch p.Verdict {
		case VerdictOnTrack:
			out.OnTrack++
		case VerdictAtRisk:
			out.AtRisk++
		case VerdictOffTrack:
			out.OffTrack++
		}
	}
	if out.TargetsSet == 0 {
		out.Reason = ReasonNoTargetsSet
		return out
	}
	if len(bands) == 0 {
		bands = DefaultClusterBands()
	}
	pct, _ := ratioPct(float64(out.OnTrack), float64(out.TargetsSet))
	out.OnTrackPct = pct
	out.Cluster = bands.clusterFor(out.OnTrack, pct)
	out.Available = true
	return out
}
