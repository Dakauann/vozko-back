package conversation

import "context"

type WhatsAppCallSignaling interface {
	InitiateCall(ctx context.Context, in WhatsAppInitiateCallInput) (string, error)

	TerminateCall(ctx context.Context, phoneID, callID string) error

	PreAcceptCall(ctx context.Context, phoneID, callID, sdpAnswer string) error

	AcceptCall(ctx context.Context, phoneID, callID, sdpAnswer, callbackData string) error

	RejectCall(ctx context.Context, phoneID, callID string) error
}

type WhatsAppInitiateCallInput struct {
	PhoneID       string
	ToPhoneNumber string
	SDPOffer      string

	CallbackData string
}

type WhatsAppCallSignalKind string

const (
	WhatsAppCallConnect WhatsAppCallSignalKind = "connect"

	WhatsAppCallRinging   WhatsAppCallSignalKind = "ringing"
	WhatsAppCallAccepted  WhatsAppCallSignalKind = "accepted"
	WhatsAppCallRejected  WhatsAppCallSignalKind = "rejected"
	WhatsAppCallTerminate WhatsAppCallSignalKind = "terminate"
)

type WhatsAppCallSignal struct {
	CallID    string
	Kind      WhatsAppCallSignalKind
	SDPAnswer string
	Reason    string
}

type WhatsAppCallWebhookHandler interface {
	HandleCallsWebhook(payload []byte) error
}

type WhatsAppInboundConnect struct {
	CallID            string
	MetaPhoneNumberID string
	FromNumber        string
	ToNumber          string
	SDPOffer          string
	ContactName       string
}

type WhatsAppInboundCallHandler interface {
	HandleInboundConnect(connect WhatsAppInboundConnect)
}

type WhatsAppCallPermissionWebhookHandler interface {
	HandleMessagesWebhook(payload []byte) error
}

type WhatsAppCallRegistry interface {
	Register(callID string) <-chan WhatsAppCallSignal

	Deliver(signal WhatsAppCallSignal)

	Unregister(callID string)
}
