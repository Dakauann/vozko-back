package copilot

import "context"

const PreviewWhatsAppTemplate = "whatsapp_template"

type Preview struct {
	Kind string      `json:"kind"`
	Data interface{} `json:"data"`
}

type Previewer interface {
	Preview(ctx context.Context, cc Context, args map[string]interface{}) *Preview
}

const PreviewMessage = "message"
