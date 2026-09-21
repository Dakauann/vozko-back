package workflow

import "vozko/domain/shared"

const VarChannel = "channel"

const ChannelBranchDefault = "default"

var ChannelBranchOrder = []shared.EntryType{
	shared.EntryTypeWhatsApp,
	shared.EntryTypeUnofficialWhatsApp,
	shared.EntryTypeInstagram,
	shared.EntryTypeTelegram,
	shared.EntryTypeSupport,
}

func ChannelOf(run *WorkflowRun) string {
	if run == nil {
		return ""
	}
	return run.EntryType
}

var channelBranchLabels = map[shared.EntryType]string{
	shared.EntryTypeWhatsApp:           "WhatsApp oficial",
	shared.EntryTypeUnofficialWhatsApp: "WhatsApp não oficial",
	shared.EntryTypeInstagram:          "Instagram",
	shared.EntryTypeTelegram:           "Telegram",
	shared.EntryTypeSupport:            "Suporte",
}

func ChannelBranchLabel(entryType shared.EntryType) string {
	if label, ok := channelBranchLabels[entryType]; ok {
		return label
	}
	return string(entryType)
}

func ChannelBranchHandles() []HandleDefinition {
	handles := make([]HandleDefinition, 0, len(ChannelBranchOrder)+1)
	for _, entryType := range ChannelBranchOrder {
		handles = append(handles, HandleDefinition{
			ID:    string(entryType),
			Label: ChannelBranchLabel(entryType),
		})
	}
	handles = append(handles, HandleDefinition{
		ID:    ChannelBranchDefault,
		Label: "Outros canais",
	})
	return handles
}
