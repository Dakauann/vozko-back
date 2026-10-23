package ses

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"vozko/domain/emailsending"
)

const lifecycleTenantARN = "arn:aws:ses:sa-east-1:123456789012:tenant/ws-1/tn-2"

type fakeLifecycleAPI struct {
	steps        []string
	failOn       string
	failWith     error
	missingAll   bool
	deletedNames []string
}

func (f *fakeLifecycleAPI) step(name string) error {
	f.steps = append(f.steps, name)
	if f.missingAll {
		return &types.NotFoundException{Message: aws.String("gone")}
	}
	if f.failOn == name {
		return f.failWith
	}
	return nil
}

func (f *fakeLifecycleAPI) DeleteTenantResourceAssociation(_ context.Context, in *sesv2.DeleteTenantResourceAssociationInput, _ ...func(*sesv2.Options)) (*sesv2.DeleteTenantResourceAssociationOutput, error) {
	return &sesv2.DeleteTenantResourceAssociationOutput{}, f.step("disassociate " + aws.ToString(in.TenantName) + "=" + aws.ToString(in.ResourceArn))
}

func (f *fakeLifecycleAPI) DeleteTenant(_ context.Context, in *sesv2.DeleteTenantInput, _ ...func(*sesv2.Options)) (*sesv2.DeleteTenantOutput, error) {
	f.deletedNames = append(f.deletedNames, aws.ToString(in.TenantName))
	return &sesv2.DeleteTenantOutput{}, f.step("delete " + aws.ToString(in.TenantName))
}

func testLifecycle(t *testing.T, api *fakeSES, lifecycle *fakeLifecycleAPI) *TenantLifecycle {
	t.Helper()
	l, err := NewTenantLifecycle(testProvider(t, api), lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

var lifecycleResources = emailsending.TenantResources{Name: "ws-1", Identities: []string{"joana.envios.test", "loja.com.br"}}

func TestNewTenantLifecycleNeedsItsClients(t *testing.T) {
	if _, err := NewTenantLifecycle(nil, &fakeLifecycleAPI{}); !errors.Is(err, ErrIncompleteConfig) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewTenantLifecycle(testProvider(t, &fakeSES{}), nil); !errors.Is(err, ErrIncompleteConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestRetireTenantDetachesEverythingBeforeDeletingTheTenant(t *testing.T) {
	lifecycle := &fakeLifecycleAPI{}
	if err := testLifecycle(t, &fakeSES{}, lifecycle).RetireTenant(context.Background(), lifecycleResources); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"disassociate ws-1=arn:aws:ses:sa-east-1:123456789012:identity/joana.envios.test",
		"disassociate ws-1=arn:aws:ses:sa-east-1:123456789012:identity/loja.com.br",
		"disassociate ws-1=arn:aws:ses:sa-east-1:123456789012:configuration-set/ws-1",
		"delete ws-1",
	}
	if !slices.Equal(lifecycle.steps, want) {
		t.Fatalf("steps = %v, want %v", lifecycle.steps, want)
	}
}

func TestRetireTenantKeepsTheTenantWhenADetachFails(t *testing.T) {
	cases := []struct {
		name   string
		failOn string
	}{
		{name: "identity detach fails", failOn: "disassociate ws-1=arn:aws:ses:sa-east-1:123456789012:identity/loja.com.br"},
		{name: "configuration set detach fails", failOn: "disassociate ws-1=arn:aws:ses:sa-east-1:123456789012:configuration-set/ws-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lifecycle := &fakeLifecycleAPI{failOn: tc.failOn, failWith: errors.New("throttled")}
			if err := testLifecycle(t, &fakeSES{}, lifecycle).RetireTenant(context.Background(), lifecycleResources); err == nil {
				t.Fatal("expected the failure")
			}
			if len(lifecycle.deletedNames) != 0 {
				t.Fatalf("the tenant was deleted after a failed detach: %v", lifecycle.steps)
			}
		})
	}
}

func TestRetireTenantPropagatesADeleteFailure(t *testing.T) {
	lifecycle := &fakeLifecycleAPI{failOn: "delete ws-1", failWith: errors.New("throttled")}
	if err := testLifecycle(t, &fakeSES{}, lifecycle).RetireTenant(context.Background(), lifecycleResources); err == nil {
		t.Fatal("expected the failure")
	}
}

func TestRetireTenantRetriedAfterAPartialRunSucceeds(t *testing.T) {
	lifecycle := &fakeLifecycleAPI{missingAll: true}
	if err := testLifecycle(t, &fakeSES{}, lifecycle).RetireTenant(context.Background(), lifecycleResources); err != nil {
		t.Fatalf("resources already gone must not fail a retry: %v", err)
	}
	if len(lifecycle.steps) != 4 {
		t.Fatalf("steps = %v", lifecycle.steps)
	}
}

func TestRetireTenantRefusesAnUnnamedTenant(t *testing.T) {
	lifecycle := &fakeLifecycleAPI{}
	err := testLifecycle(t, &fakeSES{}, lifecycle).RetireTenant(context.Background(), emailsending.TenantResources{Name: "  "})
	if !errors.Is(err, emailsending.ErrInvalidTenantName) || len(lifecycle.steps) != 0 {
		t.Fatalf("err = %v steps = %v", err, lifecycle.steps)
	}
}

func TestReviveTenantRecreatesItAndReattachesIdentitiesAndConfigurationSet(t *testing.T) {
	exists := &types.AlreadyExistsException{Message: aws.String("exists")}
	api := &fakeSES{configSetErr: exists, destinationErr: exists, tenantARN: lifecycleTenantARN}
	tenant, err := testLifecycle(t, api, &fakeLifecycleAPI{}).ReviveTenant(context.Background(), lifecycleResources)
	if err != nil {
		t.Fatal(err)
	}
	if tenant != (emailsending.Tenant{Name: "ws-1", Ref: lifecycleTenantARN}) {
		t.Fatalf("tenant = %+v", tenant)
	}
	if api.tenantInput == nil || aws.ToString(api.tenantInput.TenantName) != "ws-1" {
		t.Fatalf("the tenant was not recreated: %+v", api.tenantInput)
	}
	want := []string{
		"ws-1=arn:aws:ses:sa-east-1:123456789012:configuration-set/ws-1",
		"ws-1=arn:aws:ses:sa-east-1:123456789012:identity/joana.envios.test",
		"ws-1=arn:aws:ses:sa-east-1:123456789012:identity/loja.com.br",
	}
	if !slices.Equal(api.associations, want) {
		t.Fatalf("associations = %v, want %v", api.associations, want)
	}
}

func TestReviveTenantPropagatesFailures(t *testing.T) {
	cases := []struct {
		name string
		api  *fakeSES
	}{
		{name: "tenant creation fails", api: &fakeSES{createTenantErr: errors.New("boom"), tenantARN: lifecycleTenantARN}},
		{name: "association fails", api: &fakeSES{associateErr: errors.New("boom"), tenantARN: lifecycleTenantARN}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := testLifecycle(t, tc.api, &fakeLifecycleAPI{}).ReviveTenant(context.Background(), lifecycleResources); err == nil {
				t.Fatal("expected the failure")
			}
		})
	}
}
