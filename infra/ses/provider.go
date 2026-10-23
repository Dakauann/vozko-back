package ses

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

const (
	ProviderName         = "ses"
	eventDestinationName = "platform-events"
	dkimTokenCount       = 3
	spfValue             = "v=spf1 include:amazonses.com ~all"
	mailFromMXPriority   = 10
)

var (
	ErrIncompleteDKIM    = errors.New("ses: identity returned fewer than three dkim tokens")
	ErrTenantWithoutARN  = errors.New("ses: tenant has no arn")
	ErrIncompleteConfig  = errors.New("ses: incomplete configuration")
	ErrUnknownSendStatus = errors.New("ses: unknown sending status")
)

var publishedEvents = []types.EventType{
	types.EventTypeSend,
	types.EventTypeDelivery,
	types.EventTypeBounce,
	types.EventTypeComplaint,
	types.EventTypeReject,
	types.EventTypeOpen,
	types.EventTypeClick,
	types.EventTypeRenderingFailure,
	types.EventTypeDeliveryDelay,
}

var tenantSuppressedReasons = []types.SuppressionListReason{types.SuppressionListReasonBounce, types.SuppressionListReasonComplaint}

type SESv2API interface {
	CreateTenant(ctx context.Context, in *sesv2.CreateTenantInput, opts ...func(*sesv2.Options)) (*sesv2.CreateTenantOutput, error)
	GetTenant(ctx context.Context, in *sesv2.GetTenantInput, opts ...func(*sesv2.Options)) (*sesv2.GetTenantOutput, error)
	CreateConfigurationSet(ctx context.Context, in *sesv2.CreateConfigurationSetInput, opts ...func(*sesv2.Options)) (*sesv2.CreateConfigurationSetOutput, error)
	CreateConfigurationSetEventDestination(ctx context.Context, in *sesv2.CreateConfigurationSetEventDestinationInput, opts ...func(*sesv2.Options)) (*sesv2.CreateConfigurationSetEventDestinationOutput, error)
	CreateTenantResourceAssociation(ctx context.Context, in *sesv2.CreateTenantResourceAssociationInput, opts ...func(*sesv2.Options)) (*sesv2.CreateTenantResourceAssociationOutput, error)
	DeleteTenantResourceAssociation(ctx context.Context, in *sesv2.DeleteTenantResourceAssociationInput, opts ...func(*sesv2.Options)) (*sesv2.DeleteTenantResourceAssociationOutput, error)
	CreateEmailIdentity(ctx context.Context, in *sesv2.CreateEmailIdentityInput, opts ...func(*sesv2.Options)) (*sesv2.CreateEmailIdentityOutput, error)
	GetEmailIdentity(ctx context.Context, in *sesv2.GetEmailIdentityInput, opts ...func(*sesv2.Options)) (*sesv2.GetEmailIdentityOutput, error)
	DeleteEmailIdentity(ctx context.Context, in *sesv2.DeleteEmailIdentityInput, opts ...func(*sesv2.Options)) (*sesv2.DeleteEmailIdentityOutput, error)
	PutEmailIdentityMailFromAttributes(ctx context.Context, in *sesv2.PutEmailIdentityMailFromAttributesInput, opts ...func(*sesv2.Options)) (*sesv2.PutEmailIdentityMailFromAttributesOutput, error)
	PutEmailIdentityConfigurationSetAttributes(ctx context.Context, in *sesv2.PutEmailIdentityConfigurationSetAttributesInput, opts ...func(*sesv2.Options)) (*sesv2.PutEmailIdentityConfigurationSetAttributesOutput, error)
	SendEmail(ctx context.Context, in *sesv2.SendEmailInput, opts ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
	GetReputationEntity(ctx context.Context, in *sesv2.GetReputationEntityInput, opts ...func(*sesv2.Options)) (*sesv2.GetReputationEntityOutput, error)
	UpdateReputationEntityCustomerManagedStatus(ctx context.Context, in *sesv2.UpdateReputationEntityCustomerManagedStatusInput, opts ...func(*sesv2.Options)) (*sesv2.UpdateReputationEntityCustomerManagedStatusOutput, error)
	ListRecommendations(ctx context.Context, in *sesv2.ListRecommendationsInput, opts ...func(*sesv2.Options)) (*sesv2.ListRecommendationsOutput, error)
	PutSuppressedDestination(ctx context.Context, in *sesv2.PutSuppressedDestinationInput, opts ...func(*sesv2.Options)) (*sesv2.PutSuppressedDestinationOutput, error)
	DeleteSuppressedDestination(ctx context.Context, in *sesv2.DeleteSuppressedDestinationInput, opts ...func(*sesv2.Options)) (*sesv2.DeleteSuppressedDestinationOutput, error)
}

type Config struct {
	Region         string
	AccountID      string
	EventsTopicARN string
	Now            func() time.Time
}

type Provider struct {
	api SESv2API
	cfg Config
}

var _ emailsending.Provider = (*Provider)(nil)

func New(api SESv2API, cfg Config) (*Provider, error) {
	if api == nil {
		return nil, fmt.Errorf("%w: clients", ErrIncompleteConfig)
	}
	for name, v := range map[string]string{
		"region": cfg.Region, "account": cfg.AccountID, "events topic": cfg.EventsTopicARN,
	} {
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteConfig, name)
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Provider{api: api, cfg: cfg}, nil
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) arn(resource string) string {
	return "arn:aws:ses:" + p.cfg.Region + ":" + p.cfg.AccountID + ":" + resource
}

func (p *Provider) EnsureTenant(ctx context.Context, spec emailsending.TenantSpec) (emailsending.Tenant, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return emailsending.Tenant{}, emailsending.ErrInvalidTenantName
	}
	_, err := p.api.CreateTenant(ctx, &sesv2.CreateTenantInput{
		TenantName: aws.String(name),
		SuppressionAttributes: &types.TenantSuppressionAttributes{
			SuppressionScope:  types.SuppressionListScopeTenant,
			SuppressedReasons: tenantSuppressedReasons,
		},
	})
	if err := ignoreAlreadyExists(err); err != nil {
		return emailsending.Tenant{}, fmt.Errorf("ses: create tenant: %w", err)
	}
	_, err = p.api.CreateConfigurationSet(ctx, &sesv2.CreateConfigurationSetInput{
		ConfigurationSetName: aws.String(name),
		SendingOptions:       &types.SendingOptions{SendingEnabled: true},
		ReputationOptions:    &types.ReputationOptions{ReputationMetricsEnabled: true},
		SuppressionOptions: &types.SuppressionOptions{
			SuppressionScope:  types.SuppressionListScopeTenant,
			SuppressedReasons: tenantSuppressedReasons,
		},
	})
	if err := ignoreAlreadyExists(err); err != nil {
		return emailsending.Tenant{}, fmt.Errorf("ses: create configuration set: %w", err)
	}
	_, err = p.api.CreateConfigurationSetEventDestination(ctx, &sesv2.CreateConfigurationSetEventDestinationInput{
		ConfigurationSetName: aws.String(name),
		EventDestinationName: aws.String(eventDestinationName),
		EventDestination: &types.EventDestinationDefinition{
			Enabled:            true,
			MatchingEventTypes: publishedEvents,
			SnsDestination:     &types.SnsDestination{TopicArn: aws.String(p.cfg.EventsTopicARN)},
		},
	})
	if err := ignoreAlreadyExists(err); err != nil {
		return emailsending.Tenant{}, fmt.Errorf("ses: create event destination: %w", err)
	}
	if err := p.associate(ctx, name, p.arn("configuration-set/"+name)); err != nil {
		return emailsending.Tenant{}, fmt.Errorf("ses: associate configuration set: %w", err)
	}
	arn, err := p.tenantARN(ctx, name)
	if err != nil {
		return emailsending.Tenant{}, err
	}
	return emailsending.Tenant{Name: name, Ref: arn}, nil
}

func (p *Provider) associate(ctx context.Context, tenant, resourceARN string) error {
	_, err := p.api.CreateTenantResourceAssociation(ctx, &sesv2.CreateTenantResourceAssociationInput{
		TenantName:  aws.String(tenant),
		ResourceArn: aws.String(resourceARN),
	})
	return ignoreAlreadyExists(err)
}

func (p *Provider) tenantARN(ctx context.Context, tenant string) (string, error) {
	out, err := p.api.GetTenant(ctx, &sesv2.GetTenantInput{TenantName: aws.String(tenant)})
	if err != nil {
		return "", fmt.Errorf("ses: get tenant: %w", err)
	}
	if out == nil || out.Tenant == nil || aws.ToString(out.Tenant.TenantArn) == "" {
		return "", ErrTenantWithoutARN
	}
	return aws.ToString(out.Tenant.TenantArn), nil
}

func (p *Provider) EnsureIdentity(ctx context.Context, spec emailsending.IdentitySpec) (emailsending.IdentitySetup, error) {
	if strings.TrimSpace(spec.TenantName) == "" || strings.TrimSpace(spec.Domain) == "" || strings.TrimSpace(spec.MailFromDomain) == "" {
		return emailsending.IdentitySetup{}, fmt.Errorf("%w: identity spec", ErrIncompleteConfig)
	}
	_, err := p.api.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{
		EmailIdentity:         aws.String(spec.Domain),
		ConfigurationSetName:  aws.String(spec.TenantName),
		DkimSigningAttributes: &types.DkimSigningAttributes{NextSigningKeyLength: types.DkimSigningKeyLengthRsa2048Bit},
	})
	if err := ignoreAlreadyExists(err); err != nil {
		return emailsending.IdentitySetup{}, fmt.Errorf("ses: create identity: %w", err)
	}
	if _, err := p.api.PutEmailIdentityConfigurationSetAttributes(ctx, &sesv2.PutEmailIdentityConfigurationSetAttributesInput{
		EmailIdentity:        aws.String(spec.Domain),
		ConfigurationSetName: aws.String(spec.TenantName),
	}); err != nil {
		return emailsending.IdentitySetup{}, fmt.Errorf("ses: default configuration set: %w", err)
	}
	if _, err := p.api.PutEmailIdentityMailFromAttributes(ctx, &sesv2.PutEmailIdentityMailFromAttributesInput{
		EmailIdentity:       aws.String(spec.Domain),
		MailFromDomain:      aws.String(spec.MailFromDomain),
		BehaviorOnMxFailure: types.BehaviorOnMxFailureUseDefaultValue,
	}); err != nil {
		return emailsending.IdentitySetup{}, fmt.Errorf("ses: mail from: %w", err)
	}
	if err := p.associate(ctx, spec.TenantName, p.arn("identity/"+spec.Domain)); err != nil {
		return emailsending.IdentitySetup{}, fmt.Errorf("ses: associate identity: %w", err)
	}
	out, err := p.api.GetEmailIdentity(ctx, &sesv2.GetEmailIdentityInput{EmailIdentity: aws.String(spec.Domain)})
	if err != nil {
		return emailsending.IdentitySetup{}, fmt.Errorf("ses: get identity: %w", err)
	}
	if out == nil || out.DkimAttributes == nil || len(out.DkimAttributes.Tokens) < dkimTokenCount {
		return emailsending.IdentitySetup{}, ErrIncompleteDKIM
	}
	records := make([]emailsending.DNSRecord, 0, dkimTokenCount+2)
	for _, token := range out.DkimAttributes.Tokens[:dkimTokenCount] {
		records = append(records, emailsending.DNSRecord{
			Purpose: emailsending.RecordDKIM,
			Type:    "CNAME",
			Name:    token + "._domainkey." + spec.Domain,
			Value:   token + ".dkim.amazonses.com",
		})
	}
	records = append(records,
		emailsending.DNSRecord{
			Purpose:  emailsending.RecordMailFromMX,
			Type:     "MX",
			Name:     spec.MailFromDomain,
			Value:    "feedback-smtp." + p.cfg.Region + ".amazonses.com",
			Priority: mailFromMXPriority,
		},
		emailsending.DNSRecord{
			Purpose: emailsending.RecordMailFromSPF,
			Type:    "TXT",
			Name:    spec.MailFromDomain,
			Value:   spfValue,
		},
	)
	return emailsending.IdentitySetup{Records: records, InboundHost: "inbound-smtp." + p.cfg.Region + ".amazonaws.com"}, nil
}

func (p *Provider) IdentityStatus(ctx context.Context, domain string) (emailsending.IdentityState, error) {
	out, err := p.api.GetEmailIdentity(ctx, &sesv2.GetEmailIdentityInput{EmailIdentity: aws.String(domain)})
	if err != nil {
		var notFound *types.NotFoundException
		if errors.As(err, &notFound) {
			return emailsending.IdentityState{}, emailsending.ErrDomainNotFound
		}
		return emailsending.IdentityState{}, fmt.Errorf("ses: get identity: %w", err)
	}
	var state emailsending.IdentityState
	if out.DkimAttributes != nil {
		switch out.DkimAttributes.Status {
		case types.DkimStatusSuccess:
			state.DKIMVerified = true
		case types.DkimStatusFailed:
			state.Failed = true
			state.FailureReason = "dkim_failed"
		}
	}
	if out.MailFromAttributes != nil {
		switch out.MailFromAttributes.MailFromDomainStatus {
		case types.MailFromDomainStatusSuccess:
			state.MailFromVerified = true
		case types.MailFromDomainStatusFailed:
			state.Failed = true
			if state.FailureReason == "" {
				state.FailureReason = "mail_from_failed"
			}
		}
	}
	return state, nil
}

func (p *Provider) RemoveIdentity(ctx context.Context, tenantName, domain string) error {
	_, err := p.api.DeleteTenantResourceAssociation(ctx, &sesv2.DeleteTenantResourceAssociationInput{
		TenantName:  aws.String(tenantName),
		ResourceArn: aws.String(p.arn("identity/" + domain)),
	})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("ses: remove identity association: %w", err)
	}
	_, err = p.api.DeleteEmailIdentity(ctx, &sesv2.DeleteEmailIdentityInput{EmailIdentity: aws.String(domain)})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("ses: delete identity: %w", err)
	}
	return nil
}

func (p *Provider) Send(ctx context.Context, msg emailsending.OutboundMessage) (emailsending.SendReceipt, error) {
	if err := msg.Validate(); err != nil {
		return emailsending.SendReceipt{}, &emailsending.SendError{Kind: emailsending.SendErrorRejected, Detail: err.Error()}
	}
	messageID := msg.Tags[emailsending.MessageIDTag]
	if _, err := uuid.Parse(messageID); err != nil {
		messageID = uuid.NewString()
	}
	raw, err := buildRawMessage(msg, messageID, p.cfg.Now())
	if err != nil {
		return emailsending.SendReceipt{}, &emailsending.SendError{Kind: emailsending.SendErrorRejected, Detail: err.Error()}
	}
	out, err := p.api.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress:     aws.String(string(msg.From.Address)),
		Destination:          &types.Destination{ToAddresses: []string{string(msg.To)}},
		Content:              &types.EmailContent{Raw: &types.RawMessage{Data: raw}},
		ConfigurationSetName: aws.String(msg.TenantName),
		TenantName:           aws.String(msg.TenantName),
		EmailTags:            sanitizeTags(msg.Tags),
	})
	if err != nil {
		return emailsending.SendReceipt{}, classifySendError(err)
	}
	if out == nil || aws.ToString(out.MessageId) == "" {
		return emailsending.SendReceipt{}, &emailsending.SendError{Kind: emailsending.SendErrorTransient, Detail: "provider returned no message id"}
	}
	return emailsending.SendReceipt{ProviderMessageID: aws.ToString(out.MessageId)}, nil
}

func (p *Provider) SetTenantSending(ctx context.Context, tenantName string, enabled bool) error {
	arn, err := p.tenantARN(ctx, tenantName)
	if err != nil {
		return err
	}
	status := types.SendingStatusDisabled
	if enabled {
		status = types.SendingStatusEnabled
	}
	if _, err := p.api.UpdateReputationEntityCustomerManagedStatus(ctx, &sesv2.UpdateReputationEntityCustomerManagedStatusInput{
		ReputationEntityType:      types.ReputationEntityTypeResource,
		ReputationEntityReference: aws.String(arn),
		SendingStatus:             status,
	}); err != nil {
		return fmt.Errorf("ses: update tenant sending: %w", err)
	}
	return nil
}

func (p *Provider) TenantHealth(ctx context.Context, tenantName string) (emailsending.TenantHealth, error) {
	arn, err := p.tenantARN(ctx, tenantName)
	if err != nil {
		return emailsending.TenantHealth{}, err
	}
	entity, err := p.api.GetReputationEntity(ctx, &sesv2.GetReputationEntityInput{
		ReputationEntityType:      types.ReputationEntityTypeResource,
		ReputationEntityReference: aws.String(arn),
	})
	if err != nil {
		return emailsending.TenantHealth{}, fmt.Errorf("ses: reputation entity: %w", err)
	}
	if entity == nil || entity.ReputationEntity == nil {
		return emailsending.TenantHealth{}, fmt.Errorf("%w: empty reputation entity", ErrUnknownSendStatus)
	}
	sending, err := sendingStatusOf(entity.ReputationEntity.SendingStatusAggregate)
	if err != nil {
		return emailsending.TenantHealth{}, err
	}
	health := emailsending.TenantHealth{Sending: sending}
	var next *string
	for {
		page, err := p.api.ListRecommendations(ctx, &sesv2.ListRecommendationsInput{
			Filter:    map[string]string{string(types.ListRecommendationsFilterKeyResourceArn): arn, string(types.ListRecommendationsFilterKeyStatus): string(types.RecommendationStatusOpen)},
			NextToken: next,
		})
		if err != nil {
			return emailsending.TenantHealth{}, fmt.Errorf("ses: list recommendations: %w", err)
		}
		for _, r := range page.Recommendations {
			impact := emailsending.FindingLow
			if r.Impact == types.RecommendationImpactHigh {
				impact = emailsending.FindingHigh
			}
			health.Findings = append(health.Findings, emailsending.FindingEvent{
				TenantName:  tenantName,
				Impact:      impact,
				Type:        string(r.Type),
				Description: aws.ToString(r.Description),
				Open:        r.Status == types.RecommendationStatusOpen,
				OccurredAt:  aws.ToTime(r.LastUpdatedTimestamp),
			})
		}
		if aws.ToString(page.NextToken) == "" {
			break
		}
		next = page.NextToken
	}
	return health, nil
}

func sendingStatusOf(s types.SendingStatus) (emailsending.TenantSendingStatus, error) {
	switch s {
	case types.SendingStatusEnabled:
		return emailsending.TenantEnabled, nil
	case types.SendingStatusDisabled:
		return emailsending.TenantDisabled, nil
	case types.SendingStatusReinstated:
		return emailsending.TenantReinstated, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownSendStatus, string(s))
}

func (p *Provider) SuppressAddress(ctx context.Context, tenantName string, address emailaddress.Address, reason emailsending.SuppressReason) error {
	sesReason := types.SuppressionListReasonComplaint
	if reason == emailsending.SuppressHardBounce {
		sesReason = types.SuppressionListReasonBounce
	}
	if _, err := p.api.PutSuppressedDestination(ctx, &sesv2.PutSuppressedDestinationInput{
		EmailAddress: aws.String(string(address)),
		Reason:       sesReason,
		TenantName:   aws.String(tenantName),
	}); err != nil {
		return fmt.Errorf("ses: suppress: %w", err)
	}
	return nil
}

func (p *Provider) UnsuppressAddress(ctx context.Context, tenantName string, address emailaddress.Address) error {
	_, err := p.api.DeleteSuppressedDestination(ctx, &sesv2.DeleteSuppressedDestinationInput{
		EmailAddress: aws.String(string(address)),
		TenantName:   aws.String(tenantName),
	})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("ses: unsuppress: %w", err)
	}
	return nil
}

func ignoreAlreadyExists(err error) error {
	var exists *types.AlreadyExistsException
	if err == nil || errors.As(err, &exists) {
		return nil
	}
	return err
}

func ignoreNotFound(err error) error {
	var missing *types.NotFoundException
	if err == nil || errors.As(err, &missing) {
		return nil
	}
	return err
}
