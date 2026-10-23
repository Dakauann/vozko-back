package ses

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	sesv1 "github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"

	es "vozko/domain/emailsending"
)

const sesCallAttempts = 3

type Credentials struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	EndpointURL     string
}

type Clients struct {
	Config   aws.Config
	SES      *sesv2.Client
	Receipts *sesv1.Client
	Objects  *s3.Client
}

func NewClients(ctx context.Context, c Credentials, limiter es.ProviderCallLimiter) (Clients, error) {
	if strings.TrimSpace(c.Region) == "" || strings.TrimSpace(c.AccessKeyID) == "" || strings.TrimSpace(c.SecretAccessKey) == "" {
		return Clients{}, fmt.Errorf("%w: aws credentials", ErrIncompleteConfig)
	}
	if limiter == nil {
		return Clients{}, fmt.Errorf("%w: management call limiter", ErrIncompleteConfig)
	}
	paced := PaceManagementCalls(limiter)
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(c.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, "")),
	)
	if err != nil {
		return Clients{}, fmt.Errorf("ses: aws config: %w", err)
	}
	endpoint := strings.TrimSpace(c.EndpointURL)
	return Clients{
		Config: cfg,
		SES: sesv2.NewFromConfig(cfg, func(o *sesv2.Options) {
			o.RetryMode = aws.RetryModeStandard
			o.RetryMaxAttempts = sesCallAttempts
			o.APIOptions = append(o.APIOptions, paced)
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
		}),
		Receipts: sesv1.NewFromConfig(cfg, func(o *sesv1.Options) {
			o.RetryMode = aws.RetryModeStandard
			o.RetryMaxAttempts = sesCallAttempts
			o.APIOptions = append(o.APIOptions, paced)
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
		}),
		Objects: s3.NewFromConfig(cfg, func(o *s3.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
				o.UsePathStyle = true
			}
		}),
	}, nil
}
