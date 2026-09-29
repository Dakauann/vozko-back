package voipinfra

import (
	"context"
	"strings"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/sip_trunk"
)

func (m *SIPTrunkManager) handleInboundDialog(conn *trunkConnection, dialog *diago.DialogServerSession) {
	source := stripHostPort(dialog.InviteRequest.Source())
	if !conn.inboundSources.Contains(source) {
		m.log.Printf("trunk %s: rejected INVITE from unknown source %s", conn.trunk.ID, source)
		_ = dialog.Respond(sip.StatusForbidden, "Forbidden", nil)
		return
	}
	if !conn.trunk.TrunkType.Supports(sip_trunk.CallDirectionInbound) {
		_ = dialog.Respond(sip.StatusForbidden, "Forbidden", nil)
		return
	}
	handler := m.currentInboundHandler()
	if handler == nil {
		_ = dialog.Respond(sip.StatusTemporarilyUnavailable, "Temporarily Unavailable", nil)
		return
	}

	from := strings.TrimSpace(dialog.FromUser())
	call := m.newTrackedCall(conn, sip_trunk.CallDirectionInbound, from, &dialog.DialogMedia, dialog.Hangup, dialog.Close)
	call.setID(dialog.ID)
	invite := sip_trunk.InboundInvite{
		ID:          dialog.ID,
		TrunkID:     conn.trunk.ID,
		WorkspaceID: conn.trunk.WorkspaceID,
		FromNumber:  from,
		ToNumber:    strings.TrimSpace(dialog.ToUser()),
		ReceivedAt:  time.Now(),
		Trunk:       conn.trunk,
		Dialog:      &inboundDialog{manager: m, call: call, dialog: dialog},
	}
	if err := handler.HandleInboundInvite(conn.ctx, invite); err != nil {
		m.log.Printf("trunk %s inbound call %s: handler failed: %v", conn.trunk.ID, dialog.ID, err)
	}
}

type inboundDialog struct {
	manager *SIPTrunkManager
	call    *trackedCall
	dialog  *diago.DialogServerSession
}

var _ sip_trunk.InboundDialog = (*inboundDialog)(nil)

func (d *inboundDialog) ID() string       { return d.dialog.ID }
func (d *inboundDialog) FromUser() string { return d.dialog.FromUser() }
func (d *inboundDialog) ToUser() string   { return d.dialog.ToUser() }
func (d *inboundDialog) Trying() error    { return d.dialog.Trying() }
func (d *inboundDialog) Ringing() error   { return d.dialog.Ringing() }

func (d *inboundDialog) Answer(_ context.Context) (sip_trunk.TrunkCallSession, error) {
	if err := d.dialog.AnswerOptions(diago.AnswerOptions{
		OnMediaUpdate: d.call.onMediaUpdate,
		Codecs:        d.call.conn.runtime.Codecs,
	}); err != nil {
		_ = d.dialog.Close()
		return sip_trunk.TrunkCallSession{}, err
	}
	stream, err := d.manager.establish(d.call)
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), hangupTimeout)
		defer cancel()
		_ = d.call.terminate(ctx)
		return sip_trunk.TrunkCallSession{}, err
	}
	return d.manager.callSession(d.call, stream), nil
}

func (d *inboundDialog) Hangup(ctx context.Context) error {
	return d.call.terminate(ctx)
}

func (d *inboundDialog) Done() <-chan struct{} {
	return d.dialog.Context().Done()
}
