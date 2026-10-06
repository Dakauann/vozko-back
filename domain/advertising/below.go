package advertising

import "sort"

func (o *Object) BelowQueries() []ObjectQuery {
	base := ObjectQuery{WorkspaceID: o.WorkspaceID, AdAccountID: o.AdAccountID}
	switch o.Level {
	case LevelCampaign:
		adSets, adsBelow := base, base
		adSets.Level, adSets.CampaignIDs = LevelAdSet, []string{o.MetaID}
		adsBelow.Level, adsBelow.CampaignIDs = LevelAd, []string{o.MetaID}
		return []ObjectQuery{adSets, adsBelow}
	case LevelAdSet:
		base.Level, base.AdSetIDs = LevelAd, []string{o.MetaID}
		return []ObjectQuery{base}
	}
	return nil
}

func OffBelow(objects []*Object, parentMetaID string) []*Object {
	var off []*Object
	for _, o := range objects {
		if o.MetaID != parentMetaID && o.Status == StatusPaused {
			off = append(off, o)
		}
	}
	sort.SliceStable(off, func(i, j int) bool { return off[i].Level == LevelAdSet && off[j].Level != LevelAdSet })
	return off
}
