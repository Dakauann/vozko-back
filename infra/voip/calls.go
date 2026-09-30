package voipinfra

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/calls"
	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
	"vozko/infra/voip/audio"
)

const (
	reorderDepth   = 3
	reorderMaxWait = 60 * time.Millisecond
)

var (
	errCallEndedDuringSetup = errors.New("call ended before media was established")
	errTrunkClosing         = errors.New("trunk connection is closing")
)

const pressedKeyBuffer = 16

type trackedCall struct {
	conn        *trunkConnection
	dialogMedia *diago.DialogMedia
	hangup      func(context.Context) error
	close       func() error
	state       *calls.CallStateMachine
	done        chan struct{}
	keys        chan rune

	mu       sync.Mutex
	info     sip_trunk.ActiveCall
	latch    *latchingConn
	buffer   *audio.RTPReorderBuffer
	media    voip.PCMStream
	answered bool
	ended    bool
}

func (m *SIPTrunkManager) newTrackedCall(conn *trunkConnection, direction sip_trunk.CallDirection, number string, dialogMedia *diago.DialogMedia, hangup func(context.Context) error, closeDialog func() error) *trackedCall {
	state := calls.NewCallStateMachine(m.cfg.CallMetrics)
	state.Dialing()
	call := &trackedCall{
		conn:        conn,
		dialogMedia: dialogMedia,
		hangup:      hangup,
		close:       closeDialog,
		state:       state,
		keys:        make(chan rune, pressedKeyBuffer),
		done:        make(chan struct{}),
		info: sip_trunk.ActiveCall{
			TrunkID:     conn.trunk.ID,
			Direction:   direction,
			PhoneNumber: number,
			StartedAt:   time.Now(),
		},
	}
	dialogMedia.OnClose(call.finish)
	return call
}

func (c *trackedCall) setID(id string) {
	c.mu.Lock()
	c.info.ID = id
	c.mu.Unlock()
}

func (c *trackedCall) snapshot() sip_trunk.ActiveCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

func (c *trackedCall) finish() error {
	c.mu.Lock()
	if c.ended {
		c.mu.Unlock()
		return nil
	}
	c.ended = true
	answered, id, mediaSession := c.answered, c.info.ID, c.media
	c.mu.Unlock()

	close(c.done)
	c.conn.untrack(id)
	c.state.LeaveDialing()
	if answered {
		c.state.LeaveOngoing()
	}
	c.state.Finished()
	if mediaSession != nil {
		_ = mediaSession.Close()
	}
	return nil
}

func (c *trackedCall) terminate(ctx context.Context) error {
	hangupErr := c.hangup(ctx)
	return errors.Join(hangupErr, c.close())
}

func (c *trackedCall) onMediaUpdate(*diago.DialogMedia) {
	c.mu.Lock()
	latch := c.latch
	c.mu.Unlock()
	if latch != nil {
		latch.Reset()
	}
	if c.conn.runtime.SRTPMode == sip_trunk.SRTPModeRequired {
		c.conn.spawn(c.enforceEncryption)
	}
}

func (c *trackedCall) enforceEncryption() {
	encrypted, err := sessionEncrypted(c.dialogMedia.MediaSession())
	if err == nil && encrypted {
		return
	}
	c.conn.manager.log.Printf("trunk %s call %s: media update dropped SRTP, hanging up", c.conn.trunk.ID, c.snapshot().ID)
	ctx, cancel := context.WithTimeout(context.Background(), hangupTimeout)
	defer cancel()
	_ = c.terminate(ctx)
}

func (m *SIPTrunkManager) establish(call *trackedCall) (voip.PCMStream, error) {
	session := call.dialogMedia.MediaSession()
	latch, err := attachLatch(session)
	if err != nil {
		return nil, fmt.Errorf("attach RTP latch: %w", err)
	}
	if call.conn.runtime.SRTPMode == sip_trunk.SRTPModeRequired {
		encrypted, err := sessionEncrypted(session)
		if err != nil {
			return nil, err
		}
		if !encrypted {
			return nil, sip_trunk.ErrMediaNotEncrypted
		}
	}

	buffer := audio.NewRTPReorderBuffer(newDialogMediaView(call.dialogMedia.MediaSession), audio.RTPReorderBufferOptions{
		Depth:   reorderDepth,
		MaxWait: reorderMaxWait,
	})
	stream, err := newPCMStream(buffer, buffer.NegotiatedCodec())
	if err != nil {
		return nil, err
	}

	call.mu.Lock()
	if call.ended {
		call.mu.Unlock()
		return nil, errCallEndedDuringSetup
	}
	call.latch, call.buffer, call.media = latch, buffer, stream
	call.answered = true
	call.info.AnsweredAt = time.Now()
	call.state.Answered()
	call.conn.track(call.info.ID, call)
	call.mu.Unlock()

	buffer.OnDTMF(call.pressKey)
	buffer.Run(call.conn.ctx)
	if !call.conn.spawn(func() { m.watch(call) }) {
		return nil, errTrunkClosing
	}
	return stream, nil
}

func (m *SIPTrunkManager) watch(call *trackedCall) {
	ticker := time.NewTicker(m.limits.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-call.done:
			return
		case now := <-ticker.C:
			info := call.snapshot()
			reason := m.limits.expiry(now, info.AnsweredAt, call.buffer.LastPacketAt())
			if reason == expiryNone {
				continue
			}
			m.log.Printf("trunk %s call %s: hanging up, %s", info.TrunkID, info.ID, reason)
			ctx, cancel := context.WithTimeout(context.Background(), hangupTimeout)
			err := call.terminate(ctx)
			cancel()
			if err != nil {
				m.log.Printf("trunk %s call %s: watchdog hangup: %v", info.TrunkID, info.ID, err)
			}
			return
		}
	}
}

func (m *SIPTrunkManager) Invite(ctx context.Context, trunkID string, input sip_trunk.TrunkInviteInput) (sip_trunk.TrunkCallSession, error) {
	conn, err := m.dialableConnection(trunkID)
	if err != nil {
		return sip_trunk.TrunkCallSession{}, err
	}
	dialString, err := sip_trunk.NormalizeDialString(input.PhoneNumber)
	if err != nil {
		return sip_trunk.TrunkCallSession{}, err
	}
	number, err := sip_trunk.NormalizeDialString(conn.runtime.DialPlan.Apply(dialString))
	if err != nil {
		return sip_trunk.TrunkCallSession{}, err
	}
	domain := conn.trunk.SignalingDomain()
	dialog, err := conn.client.NewDialog(sip.Uri{User: number, Host: domain}, diago.NewDialogOptions{})
	if err != nil {
		return sip_trunk.TrunkCallSession{}, fmt.Errorf("create dialog: %w", err)
	}
	dialog.InviteRequest.SetDestination(conn.destination)
	call := m.newTrackedCall(conn, sip_trunk.CallDirectionOutbound, input.PhoneNumber, &dialog.DialogMedia, dialog.Hangup, dialog.Close)

	dialCtx, cancel := context.WithTimeout(ctx, conn.runtime.DialTimeout)
	defer cancel()
	stopOnTeardown := context.AfterFunc(conn.ctx, cancel)
	defer stopOnTeardown()

	err = dialog.Invite(dialCtx, diago.InviteClientOptions{
		Username:      conn.runtime.AuthUsername,
		Password:      conn.trunk.Password,
		Headers:       conn.runtime.inviteHeaders(conn.trunk.Username, domain),
		OnMediaUpdate: call.onMediaUpdate,
	})
	if err == nil {
		call.setID(dialog.ID)
		err = dialog.Ack(dialCtx)
	}
	if err != nil {
		_ = dialog.Close()
		return sip_trunk.TrunkCallSession{}, inviteError(err, dialCtx)
	}

	stream, err := m.establish(call)
	if err != nil {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), hangupTimeout)
		defer terminateCancel()
		_ = call.terminate(terminateCtx)
		return sip_trunk.TrunkCallSession{}, err
	}
	return m.callSession(call, stream), nil
}

func (m *SIPTrunkManager) callSession(call *trackedCall, stream voip.PCMStream) sip_trunk.TrunkCallSession {
	info := call.snapshot()
	session := call.dialogMedia.MediaSession()
	return sip_trunk.TrunkCallSession{
		ID:          info.ID,
		TrunkID:     info.TrunkID,
		PhoneNumber: info.PhoneNumber,
		Direction:   info.Direction,
		StartedAt:   info.StartedAt,
		AnsweredAt:  info.AnsweredAt,
		LocalAddr:   session.Laddr.String(),
		RemoteAddr:  session.Raddr.String(),
		Audio:       stream,
		Media:       buildMediaInfo(session),
		Keys:        call.keys,
	}
}

func (c *trackedCall) pressKey(key rune) {
	select {
	case c.keys <- key:
		return
	default:
	}
	select {
	case <-c.keys:
	default:
	}
	select {
	case c.keys <- key:
	default:
	}
}

func inviteError(err error, dialCtx context.Context) error {
	var response sipgo.ErrDialogResponse
	if errors.As(err, &response) {
		return &sip_trunk.CallRejectedError{StatusCode: response.Res.StatusCode, Reason: response.Res.Reason}
	}
	var responsePtr *sipgo.ErrDialogResponse
	if errors.As(err, &responsePtr) {
		return &sip_trunk.CallRejectedError{StatusCode: responsePtr.Res.StatusCode, Reason: responsePtr.Res.Reason}
	}
	if errors.Is(dialCtx.Err(), context.DeadlineExceeded) {
		return &sip_trunk.CallRejectedError{StatusCode: sip.StatusRequestTimeout, Reason: "No answer"}
	}
	return fmt.Errorf("invite failed: %w", err)
}

func (m *SIPTrunkManager) dialableConnection(trunkID string) (*trunkConnection, error) {
	if m.stopping() {
		return nil, sip_trunk.ErrEngineNotRunning
	}
	conn, ok := m.connection(trunkID)
	if !ok {
		return nil, sip_trunk.ErrTrunkNotRegistered
	}
	if !conn.trunk.TrunkType.Supports(sip_trunk.CallDirectionOutbound) {
		return nil, sip_trunk.ErrTrunkCannotDial
	}
	if !conn.registered() {
		return nil, sip_trunk.ErrTrunkNotRegistered
	}
	return conn, nil
}

func (m *SIPTrunkManager) Hangup(ctx context.Context, trunkID string, callID string) error {
	conn, ok := m.connection(trunkID)
	if !ok {
		return sip_trunk.ErrCallNotFound
	}
	call, ok := conn.call(callID)
	if !ok {
		return sip_trunk.ErrCallNotFound
	}
	return call.terminate(ctx)
}

func (c *trunkConnection) track(id string, call *trackedCall) {
	c.callsMu.Lock()
	c.calls[id] = call
	c.callsMu.Unlock()
}

func (c *trunkConnection) untrack(id string) {
	c.callsMu.Lock()
	delete(c.calls, id)
	c.callsMu.Unlock()
}

func (c *trunkConnection) call(id string) (*trackedCall, bool) {
	c.callsMu.Lock()
	defer c.callsMu.Unlock()
	call, ok := c.calls[id]
	return call, ok
}

func (c *trunkConnection) trackedCalls() []*trackedCall {
	c.callsMu.Lock()
	defer c.callsMu.Unlock()
	out := make([]*trackedCall, 0, len(c.calls))
	for _, call := range c.calls {
		out = append(out, call)
	}
	return out
}

func (c *trunkConnection) activeCalls() []sip_trunk.ActiveCall {
	tracked := c.trackedCalls()
	out := make([]sip_trunk.ActiveCall, 0, len(tracked))
	for _, call := range tracked {
		out = append(out, call.snapshot())
	}
	return out
}

func (c *trunkConnection) hangupAll(timeout time.Duration) {
	tracked := c.trackedCalls()
	if len(tracked) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, call := range tracked {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := call.terminate(ctx); err != nil {
				c.manager.log.Printf("trunk %s call %s: hangup during teardown: %v", c.trunk.ID, call.snapshot().ID, err)
			}
		}()
	}
	wg.Wait()
}
