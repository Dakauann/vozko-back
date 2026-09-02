package workflow

import "vozko/domain/shared"

// VarChannel is the reserved run variable naming the channel a run is executing
// on: "whatsapp", "unofficial_whatsapp", "instagram", "telegram" or "support".
//
// It is seeded from the run's EntryType when the run starts, so a workflow can
// write {{channel}} in any text field the way it already writes {{message}}. It
// is RESERVED: the seeding happens after the caller's own variables, so a
// caller cannot start a run that lies about which channel it is on. A workflow
// that sets it mid-run with action_set_variable can still shadow it for later
// interpolation — that is the author's business — but the channel branch node
// deliberately does not read it back (see ChannelOf).
const VarChannel = "channel"

// ChannelBranchDefault is the handle every channel branch carries for the
// channels its author did not name.
//
// Without it, adding a channel to the platform would silently strand every
// existing channel-branch node that predates it: the run would find no matching
// edge and stop. With it, the unnamed channels have somewhere to go and the
// author opts into handling one by drawing its edge.
const ChannelBranchDefault = "default"

// ChannelBranchOrder is the order channel handles are presented in, and the
// order the branch node declares its outputs.
//
// Every entry here must satisfy shared.EntryType.Valid(), which IS the
// messaging-channel test — and the set is bounded more tightly still by the
// workflow engine's own entry-ownership map (infra/repositories/workflow), the
// gate a webhook trigger passes before a run can exist at all. Those five types
// are the only ones a run's EntryType can ever hold.
//
// Voice is deliberately ABSENT. A telephony conversation is not a messaging
// channel — Valid() returns false for it, no trigger publishes one, and the
// ownership map has no query for it — so a voice handle could never match. It
// would be worse than useless: an author would draw an edge from it and get a
// path that silently never fires. See TestChannelBranch_OffersOnlyReachableChannels.
var ChannelBranchOrder = []shared.EntryType{
	shared.EntryTypeWhatsApp,
	shared.EntryTypeUnofficialWhatsApp,
	shared.EntryTypeInstagram,
	shared.EntryTypeTelegram,
	shared.EntryTypeSupport,
}

// ChannelOf reports the channel a run is executing on.
//
// It reads the RUN, never the state variable. The variable is a convenience for
// interpolation and an author can overwrite it; the branch that decides where a
// conversation goes must not be steerable by a typo in a set-variable node.
func ChannelOf(run *WorkflowRun) string {
	if run == nil {
		return ""
	}
	return run.EntryType
}

// channelBranchLabels are the operator-facing handle names, in the same
// Portuguese every other node definition in this package uses.
//
// A channel with no entry here still gets a handle, labelled by its raw entry
// type: a missing label is a cosmetic gap, and silently dropping the channel's
// branch would be a functional one.
var channelBranchLabels = map[shared.EntryType]string{
	shared.EntryTypeWhatsApp:           "WhatsApp oficial",
	shared.EntryTypeUnofficialWhatsApp: "WhatsApp não oficial",
	shared.EntryTypeInstagram:          "Instagram",
	shared.EntryTypeTelegram:           "Telegram",
	shared.EntryTypeSupport:            "Suporte",
}

// ChannelBranchLabel is the operator-facing name for one channel handle.
func ChannelBranchLabel(entryType shared.EntryType) string {
	if label, ok := channelBranchLabels[entryType]; ok {
		return label
	}
	return string(entryType)
}

// ChannelBranchHandles is the output handle list for the channel branch node:
// one per known channel, then the default.
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
