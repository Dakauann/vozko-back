package ses

import (
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"

	"vozko/domain/emailsending"
)

var sendErrorCodes = map[string]emailsending.SendErrorKind{
	"TooManyRequestsException":           emailsending.SendErrorTransient,
	"LimitExceededException":             emailsending.SendErrorTransient,
	"Throttling":                         emailsending.SendErrorTransient,
	"ThrottlingException":                emailsending.SendErrorTransient,
	"SendingPausedException":             emailsending.SendErrorPaused,
	"AccountSendingPausedException":      emailsending.SendErrorPaused,
	"AccountSuspendedException":          emailsending.SendErrorPaused,
	"MessageRejected":                    emailsending.SendErrorRejected,
	"MailFromDomainNotVerifiedException": emailsending.SendErrorConfiguration,
	"NotFoundException":                  emailsending.SendErrorConfiguration,
	"BadRequestException":                emailsending.SendErrorConfiguration,
	"ConfigurationSetDoesNotExist":       emailsending.SendErrorConfiguration,
	"ConfigurationSetSendingPaused":      emailsending.SendErrorPaused,
	"AccessDeniedException":              emailsending.SendErrorConfiguration,
}

func classifySendError(err error) *emailsending.SendError {
	detail := err.Error()
	var (
		throttled *types.TooManyRequestsException
		limited   *types.LimitExceededException
		paused    *types.SendingPausedException
		suspended *types.AccountSuspendedException
		rejected  *types.MessageRejected
		mailFrom  *types.MailFromDomainNotVerifiedException
		notFound  *types.NotFoundException
		bad       *types.BadRequestException
	)
	switch {
	case errors.As(err, &throttled), errors.As(err, &limited):
		return &emailsending.SendError{Kind: emailsending.SendErrorTransient, Detail: detail}
	case errors.As(err, &paused), errors.As(err, &suspended):
		return &emailsending.SendError{Kind: emailsending.SendErrorPaused, Detail: detail}
	case errors.As(err, &rejected):
		return &emailsending.SendError{Kind: emailsending.SendErrorRejected, Detail: detail}
	case errors.As(err, &mailFrom), errors.As(err, &notFound), errors.As(err, &bad):
		return &emailsending.SendError{Kind: emailsending.SendErrorConfiguration, Detail: detail}
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if kind, ok := sendErrorCodes[apiErr.ErrorCode()]; ok {
			return &emailsending.SendError{Kind: kind, Detail: detail}
		}
	}
	return &emailsending.SendError{Kind: emailsending.SendErrorTransient, Detail: detail}
}
