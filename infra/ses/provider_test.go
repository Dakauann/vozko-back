package ses

import (
	"bytes"
	"context"
	"errors"
	"net/mail"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"

	"vozko/domain/emailsending"
)

type fakeSES struct {
	calls []string

	createTenantErr  error
	configSetErr     error
	destinationErr   error
	associateErr     error
	createIdentErr   error
	deleteAssocErr   error
	deleteIdentErr   error
	sendErr          error
	identity         *sesv2.GetEmailIdentityOutput
	identityErr      error
	tenantARN        string
	aggregate        types.SendingStatus
	recommendations  [][]types.Recommendation
	recommendFilters []map[string]string

	tenantInput      *sesv2.CreateTenantInput
	configSetInput   *sesv2.CreateConfigurationSetInput
	destinationInput *sesv2.CreateConfigurationSetEventDestinationInput
	associations     []string
	identityInput    *sesv2.CreateEmailIdentityInput
	mailFromInput    *sesv2.PutEmailIdentityMailFromAttributesInput
	defaultSetInput  *sesv2.PutEmailIdentityConfigurationSetAttributesInput
	sendInput        *sesv2.SendEmailInput
	statusInput      *sesv2.UpdateReputationEntityCustomerManagedStatusInput
	reputationInput  *sesv2.GetReputationEntityInput
	suppressInput    *sesv2.PutSuppressedDestinationInput
	unsuppressInput  *sesv2.DeleteSuppressedDestinationInput
}

func (f *fakeSES) record(name string) { f.calls = append(f.calls, name) }

func (f *fakeSES) CreateTenant(_ context.Context, in *sesv2.CreateTenantInput, _ ...func(*sesv2.Options)) (*sesv2.CreateTenantOutput, error) {
	f.record("CreateTenant")
	f.tenantInput = in
	return &sesv2.CreateTenantOutput{}, f.createTenantErr
}

func (f *fakeSES) GetTenant(_ context.Context, in *sesv2.GetTenantInput, _ ...func(*sesv2.Options)) (*sesv2.GetTenantOutput, error) {
	f.record("GetTenant")
	if f.tenantARN == "" {
		return &sesv2.GetTenantOutput{Tenant: &types.Tenant{TenantName: in.TenantName}}, nil
	}
	return &sesv2.GetTenantOutput{Tenant: &types.Tenant{TenantName: in.TenantName, TenantArn: aws.String(f.tenantARN)}}, nil
}

func (f *fakeSES) CreateConfigurationSet(_ context.Context, in *sesv2.CreateConfigurationSetInput, _ ...func(*sesv2.Options)) (*sesv2.CreateConfigurationSetOutput, error) {
	f.record("CreateConfigurationSet")
	f.configSetInput = in
	return &sesv2.CreateConfigurationSetOutput{}, f.configSetErr
}

func (f *fakeSES) CreateConfigurationSetEventDestination(_ context.Context, in *sesv2.CreateConfigurationSetEventDestinationInput, _ ...func(*sesv2.Options)) (*sesv2.CreateConfigurationSetEventDestinationOutput, error) {
	f.record("CreateConfigurationSetEventDestination")
	f.destinationInput = in
	return &sesv2.CreateConfigurationSetEventDestinationOutput{}, f.destinationErr
}

func (f *fakeSES) CreateTenantResourceAssociation(_ context.Context, in *sesv2.CreateTenantResourceAssociationInput, _ ...func(*sesv2.Options)) (*sesv2.CreateTenantResourceAssociationOutput, error) {
	f.record("CreateTenantResourceAssociation")
	f.associations = append(f.associations, aws.ToString(in.TenantName)+"="+aws.ToString(in.ResourceArn))
	return &sesv2.CreateTenantResourceAssociationOutput{}, f.associateErr
}

func (f *fakeSES) DeleteTenantResourceAssociation(_ context.Context, _ *sesv2.DeleteTenantResourceAssociationInput, _ ...func(*sesv2.Options)) (*sesv2.DeleteTenantResourceAssociationOutput, error) {
	f.record("DeleteTenantResourceAssociation")
	return &sesv2.DeleteTenantResourceAssociationOutput{}, f.deleteAssocErr
}

func (f *fakeSES) CreateEmailIdentity(_ context.Context, in *sesv2.CreateEmailIdentityInput, _ ...func(*sesv2.Options)) (*sesv2.CreateEmailIdentityOutput, error) {
	f.record("CreateEmailIdentity")
	f.identityInput = in
	return &sesv2.CreateEmailIdentityOutput{}, f.createIdentErr
}

func (f *fakeSES) GetEmailIdentity(_ context.Context, _ *sesv2.GetEmailIdentityInput, _ ...func(*sesv2.Options)) (*sesv2.GetEmailIdentityOutput, error) {
	f.record("GetEmailIdentity")
	return f.identity, f.identityErr
}

func (f *fakeSES) DeleteEmailIdentity(_ context.Context, _ *sesv2.DeleteEmailIdentityInput, _ ...func(*sesv2.Options)) (*sesv2.DeleteEmailIdentityOutput, error) {
	f.record("DeleteEmailIdentity")
	return &sesv2.DeleteEmailIdentityOutput{}, f.deleteIdentErr
}

func (f *fakeSES) PutEmailIdentityMailFromAttributes(_ context.Context, in *sesv2.PutEmailIdentityMailFromAttributesInput, _ ...func(*sesv2.Options)) (*sesv2.PutEmailIdentityMailFromAttributesOutput, error) {
	f.record("PutEmailIdentityMailFromAttributes")
	f.mailFromInput = in
	return &sesv2.PutEmailIdentityMailFromAttributesOutput{}, nil
}

func (f *fakeSES) PutEmailIdentityConfigurationSetAttributes(_ context.Context, in *sesv2.PutEmailIdentityConfigurationSetAttributesInput, _ ...func(*sesv2.Options)) (*sesv2.PutEmailIdentityConfigurationSetAttributesOutput, error) {
	f.record("PutEmailIdentityConfigurationSetAttributes")
	f.defaultSetInput = in
	return &sesv2.PutEmailIdentityConfigurationSetAttributesOutput{}, nil
}

func (f *fakeSES) SendEmail(_ context.Context, in *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	f.record("SendEmail")
	f.sendInput = in
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return &sesv2.SendEmailOutput{MessageId: aws.String("ses-msg-1")}, nil
}

func (f *fakeSES) GetReputationEntity(_ context.Context, in *sesv2.GetReputationEntityInput, _ ...func(*sesv2.Options)) (*sesv2.GetReputationEntityOutput, error) {
	f.record("GetReputationEntity")
	f.reputationInput = in
	return &sesv2.GetReputationEntityOutput{ReputationEntity: &types.ReputationEntity{SendingStatusAggregate: f.aggregate}}, nil
}

func (f *fakeSES) UpdateReputationEntityCustomerManagedStatus(_ context.Context, in *sesv2.UpdateReputationEntityCustomerManagedStatusInput, _ ...func(*sesv2.Options)) (*sesv2.UpdateReputationEntityCustomerManagedStatusOutput, error) {
	f.record("UpdateReputationEntityCustomerManagedStatus")
	f.statusInput = in
	return &sesv2.UpdateReputationEntityCustomerManagedStatusOutput{}, nil
}

func (f *fakeSES) ListRecommendations(_ context.Context, in *sesv2.ListRecommendationsInput, _ ...func(*sesv2.Options)) (*sesv2.ListRecommendationsOutput, error) {
	f.record("ListRecommendations")
	f.recommendFilters = append(f.recommendFilters, in.Filter)
	page := len(f.recommendFilters) - 1
	if page >= len(f.recommendations) {
		return &sesv2.ListRecommendationsOutput{}, nil
	}
	out := &sesv2.ListRecommendationsOutput{Recommendations: f.recommendations[page]}
	if page+1 < len(f.recommendations) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func (f *fakeSES) PutSuppressedDestination(_ context.Context, in *sesv2.PutSuppressedDestinationInput, _ ...func(*sesv2.Options)) (*sesv2.PutSuppressedDestinationOutput, error) {
	f.record("PutSuppressedDestination")
	f.suppressInput = in
	return &sesv2.PutSuppressedDestinationOutput{}, nil
}

func (f *fakeSES) DeleteSuppressedDestination(_ context.Context, in *sesv2.DeleteSuppressedDestinationInput, _ ...func(*sesv2.Options)) (*sesv2.DeleteSuppressedDestinationOutput, error) {
	f.record("DeleteSuppressedDestination")
	f.unsuppressInput = in
	return &sesv2.DeleteSuppressedDestinationOutput{}, &types.NotFoundException{Message: aws.String("gone")}
}

func testProvider(t *testing.T, api *fakeSES) *Provider {
	t.Helper()
	p, err := New(api, Config{
		Region:         "sa-east-1",
		AccountID:      "123456789012",
		EventsTopicARN: "arn:aws:sns:sa-east-1:123456789012:events",
		Now:            func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewRefusesIncompleteConfig(t *testing.T) {
	if _, err := New(&fakeSES{}, Config{Region: "sa-east-1"}); !errors.Is(err, ErrIncompleteConfig) {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(nil, Config{}); !errors.Is(err, ErrIncompleteConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureTenantIsIdempotent(t *testing.T) {
	exists := &types.AlreadyExistsException{Message: aws.String("exists")}
	api := &fakeSES{createTenantErr: exists, configSetErr: exists, destinationErr: exists, associateErr: exists, tenantARN: "arn:aws:ses:sa-east-1:123456789012:tenant/ws-1/tn-1"}
	tenant, err := testProvider(t, api).EnsureTenant(context.Background(), emailsending.TenantSpec{Name: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	if tenant.Name != "ws-1" || tenant.Ref != "arn:aws:ses:sa-east-1:123456789012:tenant/ws-1/tn-1" {
		t.Fatalf("tenant = %+v", tenant)
	}
	if api.tenantInput.SuppressionAttributes.SuppressionScope != types.SuppressionListScopeTenant || len(api.tenantInput.SuppressionAttributes.SuppressedReasons) != 2 {
		t.Fatalf("suppression = %+v", api.tenantInput.SuppressionAttributes)
	}
	cs := api.configSetInput
	if aws.ToString(cs.ConfigurationSetName) != "ws-1" || !cs.SendingOptions.SendingEnabled || !cs.ReputationOptions.ReputationMetricsEnabled {
		t.Fatalf("config set = %+v", cs)
	}
	dest := api.destinationInput.EventDestination
	if !dest.Enabled || aws.ToString(dest.SnsDestination.TopicArn) != "arn:aws:sns:sa-east-1:123456789012:events" || len(dest.MatchingEventTypes) != 9 {
		t.Fatalf("destination = %+v", dest)
	}
	if len(api.associations) != 1 || api.associations[0] != "ws-1=arn:aws:ses:sa-east-1:123456789012:configuration-set/ws-1" {
		t.Fatalf("associations = %v", api.associations)
	}
}

func TestEnsureTenantPropagatesRealFailures(t *testing.T) {
	api := &fakeSES{configSetErr: errors.New("boom"), tenantARN: "arn"}
	if _, err := testProvider(t, api).EnsureTenant(context.Background(), emailsending.TenantSpec{Name: "ws-1"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := testProvider(t, &fakeSES{}).EnsureTenant(context.Background(), emailsending.TenantSpec{Name: "ws-1"}); !errors.Is(err, ErrTenantWithoutARN) {
		t.Fatalf("missing arn must fail, got %v", err)
	}
}

func TestEnsureIdentityReturnsRecords(t *testing.T) {
	api := &fakeSES{
		createIdentErr: &types.AlreadyExistsException{Message: aws.String("exists")},
		identity: &sesv2.GetEmailIdentityOutput{DkimAttributes: &types.DkimAttributes{
			Tokens: []string{"aaa", "bbb", "ccc"},
		}},
	}
	setup, err := testProvider(t, api).EnsureIdentity(context.Background(), emailsending.IdentitySpec{
		TenantName: "ws-1", Domain: "loja.com.br", MailFromDomain: "bounce.loja.com.br",
	})
	if err != nil {
		t.Fatal(err)
	}
	if api.identityInput.DkimSigningAttributes.NextSigningKeyLength != types.DkimSigningKeyLengthRsa2048Bit {
		t.Fatal("identity must use rsa 2048")
	}
	if aws.ToString(api.defaultSetInput.ConfigurationSetName) != "ws-1" {
		t.Fatal("identity must default to the tenant configuration set")
	}
	if api.mailFromInput.BehaviorOnMxFailure != types.BehaviorOnMxFailureUseDefaultValue || aws.ToString(api.mailFromInput.MailFromDomain) != "bounce.loja.com.br" {
		t.Fatalf("mail from = %+v", api.mailFromInput)
	}
	if api.associations[0] != "ws-1=arn:aws:ses:sa-east-1:123456789012:identity/loja.com.br" {
		t.Fatalf("associations = %v", api.associations)
	}
	want := []emailsending.DNSRecord{
		{Purpose: emailsending.RecordDKIM, Type: "CNAME", Name: "aaa._domainkey.loja.com.br", Value: "aaa.dkim.amazonses.com"},
		{Purpose: emailsending.RecordDKIM, Type: "CNAME", Name: "bbb._domainkey.loja.com.br", Value: "bbb.dkim.amazonses.com"},
		{Purpose: emailsending.RecordDKIM, Type: "CNAME", Name: "ccc._domainkey.loja.com.br", Value: "ccc.dkim.amazonses.com"},
		{Purpose: emailsending.RecordMailFromMX, Type: "MX", Name: "bounce.loja.com.br", Value: "feedback-smtp.sa-east-1.amazonses.com", Priority: 10},
		{Purpose: emailsending.RecordMailFromSPF, Type: "TXT", Name: "bounce.loja.com.br", Value: "v=spf1 include:amazonses.com ~all"},
	}
	if len(setup.Records) != len(want) {
		t.Fatalf("records = %+v", setup.Records)
	}
	for i := range want {
		if setup.Records[i] != want[i] {
			t.Fatalf("record %d = %+v, want %+v", i, setup.Records[i], want[i])
		}
	}
	if setup.InboundHost != "inbound-smtp.sa-east-1.amazonaws.com" {
		t.Fatalf("inbound host = %q", setup.InboundHost)
	}
}

func TestEnsureIdentityRefusesMissingDKIM(t *testing.T) {
	api := &fakeSES{identity: &sesv2.GetEmailIdentityOutput{DkimAttributes: &types.DkimAttributes{Tokens: []string{"a"}}}}
	_, err := testProvider(t, api).EnsureIdentity(context.Background(), emailsending.IdentitySpec{TenantName: "ws-1", Domain: "loja.com.br", MailFromDomain: "bounce.loja.com.br"})
	if !errors.Is(err, ErrIncompleteDKIM) {
		t.Fatalf("err = %v", err)
	}
}

func TestIdentityStatusMapping(t *testing.T) {
	cases := []struct {
		name     string
		dkim     types.DkimStatus
		mailFrom types.MailFromDomainStatus
		want     emailsending.IdentityState
	}{
		{"both verified", types.DkimStatusSuccess, types.MailFromDomainStatusSuccess, emailsending.IdentityState{DKIMVerified: true, MailFromVerified: true}},
		{"pending", types.DkimStatusPending, types.MailFromDomainStatusPending, emailsending.IdentityState{}},
		{"temporary failures are not final", types.DkimStatusTemporaryFailure, types.MailFromDomainStatusTemporaryFailure, emailsending.IdentityState{}},
		{"dkim failed", types.DkimStatusFailed, types.MailFromDomainStatusSuccess, emailsending.IdentityState{MailFromVerified: true, Failed: true, FailureReason: "dkim_failed"}},
		{"mail from failed", types.DkimStatusSuccess, types.MailFromDomainStatusFailed, emailsending.IdentityState{DKIMVerified: true, Failed: true, FailureReason: "mail_from_failed"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeSES{identity: &sesv2.GetEmailIdentityOutput{
				DkimAttributes:     &types.DkimAttributes{Status: tc.dkim},
				MailFromAttributes: &types.MailFromAttributes{MailFromDomainStatus: tc.mailFrom},
			}}
			got, err := testProvider(t, api).IdentityStatus(context.Background(), "loja.com.br")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("state = %+v, want %+v", got, tc.want)
			}
		})
	}
	api := &fakeSES{identityErr: &types.NotFoundException{Message: aws.String("no")}}
	if _, err := testProvider(t, api).IdentityStatus(context.Background(), "x.com"); !errors.Is(err, emailsending.ErrDomainNotFound) {
		t.Fatalf("not found = %v", err)
	}
}

func TestRemoveIdentityToleratesMissingResources(t *testing.T) {
	missing := &types.NotFoundException{Message: aws.String("no")}
	api := &fakeSES{deleteAssocErr: missing, deleteIdentErr: missing}
	if err := testProvider(t, api).RemoveIdentity(context.Background(), "ws-1", "loja.com.br"); err != nil {
		t.Fatal(err)
	}
	api = &fakeSES{deleteIdentErr: errors.New("boom")}
	if err := testProvider(t, api).RemoveIdentity(context.Background(), "ws-1", "loja.com.br"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSendBuildsRawRequest(t *testing.T) {
	api := &fakeSES{}
	msg := sampleOutbound()
	msg.Tags = map[string]string{"workspace": "ws-1", emailsending.MessageIDTag: "11111111-2222-3333-4444-555555555555"}
	receipt, err := testProvider(t, api).Send(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderMessageID != "ses-msg-1" {
		t.Fatalf("receipt = %+v", receipt)
	}
	in := api.sendInput
	if aws.ToString(in.TenantName) != "ws-1" || aws.ToString(in.ConfigurationSetName) != "ws-1" || in.Destination.ToAddresses[0] != "cliente@example.com" {
		t.Fatalf("input = %+v", in)
	}
	if len(in.EmailTags) != 2 {
		t.Fatalf("tags = %+v", in.EmailTags)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(in.Content.Raw.Data))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("Message-Id") != "<11111111-2222-3333-4444-555555555555@joana.envios.test>" {
		t.Fatalf("message id = %q", parsed.Header.Get("Message-Id"))
	}
}

func TestSendRefusesInvalidMessagesWithoutCallingTheProvider(t *testing.T) {
	api := &fakeSES{}
	msg := sampleOutbound()
	msg.Headers = nil
	_, err := testProvider(t, api).Send(context.Background(), msg)
	if kind, ok := emailsending.SendErrorKindOf(err); !ok || kind != emailsending.SendErrorRejected {
		t.Fatalf("err = %v", err)
	}
	if len(api.calls) != 0 {
		t.Fatalf("provider called: %v", api.calls)
	}
}

func TestSendErrorClassification(t *testing.T) {
	cases := []struct {
		err  error
		want emailsending.SendErrorKind
	}{
		{&types.TooManyRequestsException{Message: aws.String("slow")}, emailsending.SendErrorTransient},
		{&types.LimitExceededException{Message: aws.String("limit")}, emailsending.SendErrorTransient},
		{&types.SendingPausedException{Message: aws.String("paused")}, emailsending.SendErrorPaused},
		{&types.AccountSuspendedException{Message: aws.String("suspended")}, emailsending.SendErrorPaused},
		{&types.MessageRejected{Message: aws.String("rejected")}, emailsending.SendErrorRejected},
		{&types.MailFromDomainNotVerifiedException{Message: aws.String("mail from")}, emailsending.SendErrorConfiguration},
		{&types.NotFoundException{Message: aws.String("missing")}, emailsending.SendErrorConfiguration},
		{&smithy.GenericAPIError{Code: "AccountSendingPausedException", Message: "paused"}, emailsending.SendErrorPaused},
		{&smithy.GenericAPIError{Code: "Throttling", Message: "slow"}, emailsending.SendErrorTransient},
		{&smithy.GenericAPIError{Code: "SomethingNew", Message: "?"}, emailsending.SendErrorTransient},
		{context.DeadlineExceeded, emailsending.SendErrorTransient},
	}
	for _, tc := range cases {
		api := &fakeSES{sendErr: tc.err}
		_, err := testProvider(t, api).Send(context.Background(), sampleOutbound())
		if kind, ok := emailsending.SendErrorKindOf(err); !ok || kind != tc.want {
			t.Errorf("%T %v: kind %q, want %q", tc.err, tc.err, kind, tc.want)
		}
	}
}

func TestTenantSendingAndHealth(t *testing.T) {
	arn := "arn:aws:ses:sa-east-1:123456789012:tenant/ws-1/tn-1"
	api := &fakeSES{tenantARN: arn, aggregate: types.SendingStatusDisabled, recommendations: [][]types.Recommendation{
		{{Type: types.RecommendationTypeBounce, Impact: types.RecommendationImpactHigh, Status: types.RecommendationStatusOpen, Description: aws.String("bounce")}},
		{{Type: types.RecommendationTypeComplaint, Impact: types.RecommendationImpactLow, Status: types.RecommendationStatusOpen}},
	}}
	p := testProvider(t, api)
	if err := p.SetTenantSending(context.Background(), "ws-1", false); err != nil {
		t.Fatal(err)
	}
	if api.statusInput.SendingStatus != types.SendingStatusDisabled || aws.ToString(api.statusInput.ReputationEntityReference) != arn || api.statusInput.ReputationEntityType != types.ReputationEntityTypeResource {
		t.Fatalf("status input = %+v", api.statusInput)
	}
	if err := p.SetTenantSending(context.Background(), "ws-1", true); err != nil || api.statusInput.SendingStatus != types.SendingStatusEnabled {
		t.Fatalf("enable = %v %+v", err, api.statusInput)
	}
	health, err := p.TenantHealth(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if health.Sending != emailsending.TenantDisabled || len(health.Findings) != 2 || health.Findings[0].Impact != emailsending.FindingHigh || health.Findings[1].Impact != emailsending.FindingLow {
		t.Fatalf("health = %+v", health)
	}
	if api.recommendFilters[0]["RESOURCE_ARN"] != arn || api.recommendFilters[0]["STATUS"] != "OPEN" {
		t.Fatalf("filters = %v", api.recommendFilters)
	}
}

func TestTenantHealthRefusesUnknownStatus(t *testing.T) {
	api := &fakeSES{tenantARN: "arn", aggregate: types.SendingStatus("WEIRD")}
	if _, err := testProvider(t, api).TenantHealth(context.Background(), "ws-1"); !errors.Is(err, ErrUnknownSendStatus) {
		t.Fatalf("err = %v", err)
	}
}

func TestSuppressionCallsUseTheTenant(t *testing.T) {
	api := &fakeSES{}
	p := testProvider(t, api)
	if err := p.SuppressAddress(context.Background(), "ws-1", "a@b.com", emailsending.SuppressHardBounce); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(api.suppressInput.TenantName) != "ws-1" || api.suppressInput.Reason != types.SuppressionListReasonBounce {
		t.Fatalf("suppress = %+v", api.suppressInput)
	}
	if err := p.SuppressAddress(context.Background(), "ws-1", "a@b.com", emailsending.SuppressComplaint); err != nil || api.suppressInput.Reason != types.SuppressionListReasonComplaint {
		t.Fatalf("complaint = %v %+v", err, api.suppressInput)
	}
	if err := p.UnsuppressAddress(context.Background(), "ws-1", "a@b.com"); err != nil {
		t.Fatalf("missing entry must not fail: %v", err)
	}
	if aws.ToString(api.unsuppressInput.TenantName) != "ws-1" {
		t.Fatalf("unsuppress = %+v", api.unsuppressInput)
	}
}
