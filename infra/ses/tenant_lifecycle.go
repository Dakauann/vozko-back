package ses

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"

	"vozko/domain/emailsending"
)

type TenantLifecycleAPI interface {
	DeleteTenantResourceAssociation(ctx context.Context, in *sesv2.DeleteTenantResourceAssociationInput, opts ...func(*sesv2.Options)) (*sesv2.DeleteTenantResourceAssociationOutput, error)
	DeleteTenant(ctx context.Context, in *sesv2.DeleteTenantInput, opts ...func(*sesv2.Options)) (*sesv2.DeleteTenantOutput, error)
}

type TenantLifecycle struct {
	provider *Provider
	api      TenantLifecycleAPI
}

var _ emailsending.TenantLifecycle = (*TenantLifecycle)(nil)

func NewTenantLifecycle(provider *Provider, api TenantLifecycleAPI) (*TenantLifecycle, error) {
	if provider == nil || api == nil {
		return nil, fmt.Errorf("%w: tenant lifecycle clients", ErrIncompleteConfig)
	}
	return &TenantLifecycle{provider: provider, api: api}, nil
}

func (l *TenantLifecycle) RetireTenant(ctx context.Context, tenant emailsending.TenantResources) error {
	name := strings.TrimSpace(tenant.Name)
	if name == "" {
		return emailsending.ErrInvalidTenantName
	}
	for _, resource := range l.attachedResources(name, tenant.Identities) {
		_, err := l.api.DeleteTenantResourceAssociation(ctx, &sesv2.DeleteTenantResourceAssociationInput{
			TenantName:  aws.String(name),
			ResourceArn: aws.String(resource),
		})
		if err := ignoreNotFound(err); err != nil {
			return fmt.Errorf("ses: detach %s: %w", resource, err)
		}
	}
	_, err := l.api.DeleteTenant(ctx, &sesv2.DeleteTenantInput{TenantName: aws.String(name)})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("ses: delete tenant: %w", err)
	}
	return nil
}

func (l *TenantLifecycle) ReviveTenant(ctx context.Context, tenant emailsending.TenantResources) (emailsending.Tenant, error) {
	revived, err := l.provider.EnsureTenant(ctx, emailsending.TenantSpec{Name: tenant.Name})
	if err != nil {
		return emailsending.Tenant{}, err
	}
	for _, identity := range tenant.Identities {
		if err := l.provider.associate(ctx, revived.Name, l.provider.arn("identity/"+identity)); err != nil {
			return emailsending.Tenant{}, fmt.Errorf("ses: attach identity %s: %w", identity, err)
		}
	}
	return revived, nil
}

func (l *TenantLifecycle) attachedResources(tenantName string, identities []string) []string {
	resources := make([]string, 0, len(identities)+1)
	for _, identity := range identities {
		resources = append(resources, l.provider.arn("identity/"+identity))
	}
	return append(resources, l.provider.arn("configuration-set/"+tenantName))
}
