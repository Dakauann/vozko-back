package workflow

type ServiceWindow string

const (
	ServiceWindowOpen   ServiceWindow = "open"
	ServiceWindowClosed ServiceWindow = "closed"
)

const WindowClosedFix = "Fora da janela de atendimento de 24 h só um template pode falar com o contato: envie um template antes desta mensagem ou aguarde a resposta do contato."

var openWindowTriggers = map[TriggerType]bool{
	TriggerMessageReceived: true,
	TriggerFirstMessage:    true,
	TriggerAgentCall:       true,
	TriggerFormSubmitted:   true,
}

var customerMessageTriggers = map[TriggerType]bool{
	TriggerMessageReceived: true,
	TriggerFirstMessage:    true,
}

func DefaultServiceWindow(t TriggerType) *ServiceWindow {
	if t.WorkflowType() == WorkflowTypeVoice {
		return nil
	}
	window := ServiceWindowClosed
	if openWindowTriggers[t] {
		window = ServiceWindowOpen
	}
	return &window
}

func ServiceWindowEditable(t TriggerType) bool {
	return DefaultServiceWindow(t) != nil && !customerMessageTriggers[t]
}

func (s TriggerInputSpec) WindowOpen(chosen *bool) bool {
	if s.Window == nil {
		return false
	}
	if chosen != nil && s.WindowEditable {
		return *chosen
	}
	return *s.Window == ServiceWindowOpen
}
