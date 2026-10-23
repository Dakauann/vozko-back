package container

import (
	"log"

	es "vozko/domain/emailsending"
	"vozko/infra/ses"
)

func (c *Container) emailTenantLifecycle(provider *ses.Provider, api ses.TenantLifecycleAPI) es.TenantLifecycle {
	tenants, err := ses.NewTenantLifecycle(provider, api)
	if err != nil {
		log.Fatalf("[email] tenant lifecycle: %v", err)
	}
	return tenants
}
