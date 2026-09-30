package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const DTMFKeys = "0123456789*#"

var (
	ErrNotInCall        = errors.New("voice node can only run inside a call")
	ErrCallEnded        = fmt.Errorf("caller hung up: %w", context.Canceled)
	ErrInvalidDTMFKey   = errors.New("dtmf keys must be 0-9, * or #")
	ErrDuplicateDTMF    = errors.New("each dtmf key can lead to one branch only")
	ErrNoDTMFKeys       = errors.New("choose at least one key")
	ErrAudioNotPlayable = errors.New("audio file cannot be played on calls")
	ErrNotTransferable  = errors.New("this call cannot be transferred")
)

type VoiceCall interface {
	Play(pcm []byte, interruptible bool) (interrupted bool, err error)
	NextKey(timeout time.Duration) (key rune, pressed bool, err error)
}

type QueueTransfer struct {
	QueueID string
	Notes   string
	From    string
}

type VoiceTransfers interface {
	TransferToQueue(ctx context.Context, transfer QueueTransfer) (connected bool, err error)
}

func VoiceTransfersFrom(ctx *NodeContext) (VoiceTransfers, error) {
	if _, err := VoiceCallFrom(ctx); err != nil {
		return nil, err
	}
	transfers, ok := ctx.Runtime.(VoiceTransfers)
	if !ok || transfers == nil {
		return nil, ErrNotTransferable
	}
	return transfers, nil
}

type VoiceAudio interface {
	LoadPCM(ctx context.Context, workspaceID, mediaID string) ([]byte, error)
}

func VoiceCallFrom(ctx *NodeContext) (VoiceCall, error) {
	if ctx == nil {
		return nil, ErrNotInCall
	}
	call, ok := ctx.Runtime.(VoiceCall)
	if !ok || call == nil {
		return nil, ErrNotInCall
	}
	return call, nil
}

func IsDTMFKey(key string) bool {
	return len(key) == 1 && strings.Contains(DTMFKeys, key)
}

func ValidateDTMFKeys(keys []string) error {
	if len(keys) == 0 {
		return ErrNoDTMFKeys
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !IsDTMFKey(key) {
			return fmt.Errorf("%w: %q", ErrInvalidDTMFKey, key)
		}
		if seen[key] {
			return fmt.Errorf("%w: %q", ErrDuplicateDTMF, key)
		}
		seen[key] = true
	}
	return nil
}

var ErrNodeInvalidDTMFConfig = errors.New("invalid dtmf wait config")

func DTMFKeysOf(config map[string]interface{}) []string {
	raw, _ := config["keys"].([]interface{})
	keys := make([]string, 0, len(raw))
	for _, item := range raw {
		if key, ok := item.(string); ok {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	return keys
}

func ValidateDTMFWaits(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeWaitDTMF {
			continue
		}
		if err := ValidateDTMFKeys(DTMFKeysOf(n.Config)); err != nil {
			return fmt.Errorf("%w: node %q: %v", ErrNodeInvalidDTMFConfig, n.ID, err)
		}
	}
	return nil
}

var (
	ErrVoiceTrunkRequired = errors.New("voice workflow must choose a SIP trunk")
	ErrVoiceTrunkInvalid  = errors.New("SIP trunk does not belong to this workspace or cannot receive calls")
	ErrVoiceTrunkTaken    = errors.New("SIP trunk already answers calls with another active voice workflow")
)

func (w *Workflow) VoiceTrunkID() string {
	if w == nil {
		return ""
	}
	trigger := w.Graph.TriggerNodeByType(TriggerCallReceived)
	if trigger == nil {
		return ""
	}
	trunkID, _ := trigger.Config["trunk_id"].(string)
	return strings.TrimSpace(trunkID)
}

const EntryTypeSIPCall = "sip_call"

var ErrVoiceFlowAmbiguous = errors.New("more than one active voice workflow answers this trunk")

type InboundVoiceCall struct {
	WorkspaceID  string
	TrunkID      string
	CallID       string
	CallerNumber string
	CalledNumber string
	Call         VoiceCall
}

type InboundVoiceFlows interface {
	FlowFor(workspaceID, trunkID string) (*Workflow, error)
	Answer(flow *Workflow, call InboundVoiceCall) error
}

func WorkflowTypes() []WorkflowType {
	return []WorkflowType{WorkflowTypeMessages, WorkflowTypeVoice}
}
