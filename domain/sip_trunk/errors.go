package sip_trunk

import "errors"

var (
	ErrTrunkNotFound        = errors.New("sip trunk not found")
	ErrTrunkNotRegistered   = errors.New("sip trunk not registered")
	ErrTrunkDisabled        = errors.New("sip trunk is disabled")
	ErrTrunkCannotDial      = errors.New("sip trunk does not allow outbound calls")
	ErrCallNotFound         = errors.New("call not found")
	ErrCallNotPermitted     = errors.New("you are not allowed to call through this workspace's trunks")
	ErrEngineNotRunning     = errors.New("sip trunk engine is not running")
	ErrMediaNotEncrypted    = errors.New("trunk requires SRTP but the negotiated media is not encrypted")
	ErrInvalidPhoneNumber   = errors.New("phone number must contain only digits, *, # and an optional leading +")
	ErrWorkspaceRequired    = errors.New("workspace is required")
	ErrNameRequired         = errors.New("name is required")
	ErrHostRequired         = errors.New("host is required")
	ErrInvalidHost          = errors.New("host must be a bare hostname or IP address")
	ErrInvalidPort          = errors.New("port must be between 1 and 65535")
	ErrCredentialsRequired  = errors.New("username and password are required when the trunk registers")
	ErrUnsupportedTrunkType = errors.New("unsupported trunk type")
	ErrUnsupportedTransport = errors.New("unsupported SIP transport")
	ErrUnsupportedCodec     = errors.New("unsupported codec")
	ErrUnsupportedSRTPMode  = errors.New("unsupported SRTP mode")
	ErrInvalidDialPlan      = errors.New("dial plan prefixes may only contain digits, *, # and a leading +")
	ErrInvalidInboundSource = errors.New("inbound source must be an IP address or CIDR")
	ErrNoBridgeableCodec    = errors.New("codecs must include PCMU or PCMA so calls can reach the browser")
)

var invalidInputErrors = []error{
	ErrInvalidPhoneNumber,
	ErrWorkspaceRequired,
	ErrNameRequired,
	ErrHostRequired,
	ErrInvalidHost,
	ErrInvalidPort,
	ErrCredentialsRequired,
	ErrUnsupportedTrunkType,
	ErrUnsupportedTransport,
	ErrUnsupportedCodec,
	ErrUnsupportedSRTPMode,
	ErrInvalidDialPlan,
	ErrInvalidInboundSource,
	ErrNoBridgeableCodec,
}

func IsInvalidInput(err error) bool {
	for _, target := range invalidInputErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
