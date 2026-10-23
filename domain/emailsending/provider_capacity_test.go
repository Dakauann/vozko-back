package emailsending

import (
	"testing"
)

func TestProviderUsageCrossesTheAlertLineAtEightyPercent(t *testing.T) {
	cases := []struct {
		name  string
		usage ProviderUsage
		bp    int
		near  bool
	}{
		{name: "empty", usage: ProviderUsage{Used: 0, Quota: 10000}, bp: 0},
		{name: "just under", usage: ProviderUsage{Used: 7999, Quota: 10000}, bp: 7999},
		{name: "at eighty", usage: ProviderUsage{Used: 8000, Quota: 10000}, bp: 8000, near: true},
		{name: "ip filters at eighty", usage: ProviderUsage{Used: 80, Quota: 100}, bp: 8000, near: true},
		{name: "over the quota", usage: ProviderUsage{Used: 120, Quota: 100}, bp: 12000, near: true},
		{name: "no quota known counts as full", usage: ProviderUsage{Used: 1, Quota: 0}, bp: 10000, near: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.usage.BasisPoints(); got != tc.bp {
				t.Fatalf("BasisPoints = %d, want %d", got, tc.bp)
			}
			if got := tc.usage.NearQuota(); got != tc.near {
				t.Fatalf("NearQuota = %v, want %v", got, tc.near)
			}
		})
	}
}

func TestProviderUsagesCompareEveryCountWithItsSESQuota(t *testing.T) {
	usages := ProviderUsages(ProviderInventory{Identities: 1, Tenants: 2, ConfigurationSets: 3, ReplyDomains: 4, IPFilters: 5})
	want := map[ProviderResource]ProviderUsage{
		ResourceIdentities:        {Resource: ResourceIdentities, Used: 1, Quota: SESIdentitiesPerRegion},
		ResourceTenants:           {Resource: ResourceTenants, Used: 2, Quota: SESTenantsPerAccount},
		ResourceConfigurationSets: {Resource: ResourceConfigurationSets, Used: 3, Quota: SESConfigurationSetsPerRegion},
		ResourceReplyDomains:      {Resource: ResourceReplyDomains, Used: 4, Quota: InboundReplyDomainCapacity()},
		ResourceIPFilters:         {Resource: ResourceIPFilters, Used: 5, Quota: InboundIPFiltersPerAccount},
	}
	if len(usages) != len(want) {
		t.Fatalf("usages = %+v", usages)
	}
	for _, u := range usages {
		if u != want[u.Resource] {
			t.Errorf("%s = %+v, want %+v", u.Resource, u, want[u.Resource])
		}
	}
	if SESIdentitiesPerRegion != 10000 || SESTenantsPerAccount != 10000 || SESConfigurationSetsPerRegion != 10000 || InboundReplyDomainCapacity() != 10000 {
		t.Fatal("the SES quotas page lists 10,000 identities, tenants, configuration sets and 200 rules of 50 reply domains")
	}
}

func TestInventoryCountsWhatExistsAtTheProvider(t *testing.T) {
	accounts := []SenderAccount{
		{TenantName: "ws-a", Status: AccountActive},
		{TenantName: "ws-b", Status: AccountPausedByProvider},
		{TenantName: "ws-c", Status: AccountDormant},
	}
	domains := []SendingDomain{
		{Domain: "a.envios.test", ReplyDomain: "a.envios.test", Kind: DomainSharedProbation, Status: DomainVerified},
		{Domain: "old.envios.test", ReplyDomain: "old.envios.test", Kind: DomainSharedProbation, Status: DomainVerified, Retired: true},
		{Domain: "loja.com.br", ReplyDomain: "reply.loja.com.br", Kind: DomainOwn, Status: DomainVerifying},
		{Domain: "gone.com.br", ReplyDomain: "reply.gone.com.br", Kind: DomainOwn, Status: DomainVerified, Retired: true},
		{Domain: "new.com.br", ReplyDomain: "reply.new.com.br", Kind: DomainOwn, Status: DomainPending},
	}
	got := InventoryOf(accounts, domains, 7)
	want := ProviderInventory{Identities: 3, Tenants: 2, ConfigurationSets: 3, ReplyDomains: 4, IPFilters: 7}
	if got != want {
		t.Fatalf("inventory = %+v, want %+v", got, want)
	}
}

func TestOnlyUsagesNearTheirQuotaAreAlerted(t *testing.T) {
	near := UsagesNearQuota(ProviderUsages(ProviderInventory{Identities: 8000, Tenants: 7999, IPFilters: 95}))
	if len(near) != 2 || near[0].Resource != ResourceIdentities || near[1].Resource != ResourceIPFilters {
		t.Fatalf("near = %+v", near)
	}
}
