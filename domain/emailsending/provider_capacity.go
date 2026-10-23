package emailsending

const (
	SESTenantsPerAccount          = 10000
	SESIdentitiesPerRegion        = 10000
	SESConfigurationSetsPerRegion = 10000
	CapacityAlertBasisPoints      = 8000
)

type ProviderResource string

const (
	ResourceIdentities        ProviderResource = "verified_identities"
	ResourceTenants           ProviderResource = "tenants"
	ResourceConfigurationSets ProviderResource = "configuration_sets"
	ResourceReplyDomains      ProviderResource = "reply_domains"
	ResourceIPFilters         ProviderResource = "ip_filters"
)

type ProviderUsage struct {
	Resource ProviderResource
	Used     int
	Quota    int
}

func (u ProviderUsage) BasisPoints() int {
	if u.Quota <= 0 {
		return basisPointsWhole
	}
	return u.Used * basisPointsWhole / u.Quota
}

func (u ProviderUsage) NearQuota() bool {
	return u.BasisPoints() >= CapacityAlertBasisPoints
}

type ProviderInventory struct {
	Identities        int
	Tenants           int
	ConfigurationSets int
	ReplyDomains      int
	IPFilters         int
}

func ProviderUsages(inv ProviderInventory) []ProviderUsage {
	return []ProviderUsage{
		{Resource: ResourceIdentities, Used: inv.Identities, Quota: SESIdentitiesPerRegion},
		{Resource: ResourceTenants, Used: inv.Tenants, Quota: SESTenantsPerAccount},
		{Resource: ResourceConfigurationSets, Used: inv.ConfigurationSets, Quota: SESConfigurationSetsPerRegion},
		{Resource: ResourceReplyDomains, Used: inv.ReplyDomains, Quota: InboundReplyDomainCapacity()},
		{Resource: ResourceIPFilters, Used: inv.IPFilters, Quota: InboundIPFiltersPerAccount},
	}
}

func UsagesNearQuota(usages []ProviderUsage) []ProviderUsage {
	var near []ProviderUsage
	for _, u := range usages {
		if u.NearQuota() {
			near = append(near, u)
		}
	}
	return near
}

func InventoryOf(accounts []SenderAccount, domains []SendingDomain, ipFilters int) ProviderInventory {
	inv := ProviderInventory{ConfigurationSets: len(accounts), IPFilters: ipFilters}
	for _, a := range accounts {
		if a.Status != AccountDormant {
			inv.Tenants++
		}
	}
	replyDomains := map[string]bool{}
	for _, d := range domains {
		if d.HoldsProviderIdentity() {
			inv.Identities++
		}
		if d.ReceivesReplies() && d.ReplyDomain != "" {
			replyDomains[d.ReplyDomain] = true
		}
	}
	inv.ReplyDomains = len(replyDomains)
	return inv
}

func (d SendingDomain) HoldsProviderIdentity() bool {
	if d.Retired && !d.Kind.Shared() {
		return false
	}
	switch d.Status {
	case DomainVerifying, DomainVerified, DomainFailed:
		return true
	}
	return false
}
