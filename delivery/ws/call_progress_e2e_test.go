package ws

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/voip"
	"vozko/infra/voip/voiptest"
)

type socketMessage struct {
	Type    WSEventType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (c *dialerClient) next() socketMessage {
	c.t.Helper()
	select {
	case raw, ok := <-c.raw:
		if !ok {
			c.t.Fatal("socket closed")
		}
		var message socketMessage
		_ = json.Unmarshal(raw, &message)
		return message
	case <-time.After(8 * time.Second):
		c.t.Fatal("the socket went quiet")
		return socketMessage{}
	}
}

func audioOf(t *testing.T, message socketMessage) []byte {
	t.Helper()
	var out CallAudioOutPayload
	if err := json.Unmarshal(message.Payload, &out); err != nil {
		t.Fatal(err)
	}
	pcm, err := base64.StdEncoding.DecodeString(out.Audio)
	if err != nil {
		t.Fatal(err)
	}
	return pcm
}

func callStatusOf(message socketMessage) string {
	var status CallStatusPayload
	_ = json.Unmarshal(message.Payload, &status)
	return string(status.Status)
}

func isTone(pcm []byte) bool {
	crossings := 0
	for i := 2; i+1 < len(pcm); i += 2 {
		previous := int16(binary.LittleEndian.Uint16(pcm[i-2:]))
		current := int16(binary.LittleEndian.Uint16(pcm[i:]))
		if (previous < 0) != (current < 0) {
			crossings++
		}
	}
	return crossings >= 12 && crossings <= 22
}

func TestTheOperatorHearsTheStandardRingbackWhileTheFarEndRings(t *testing.T) {
	stack := startDialerStack(t)
	answer := make(chan struct{})
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		<-answer
		if err := d.Answer(); err != nil {
			return
		}
		go voiptest.SendRTP(d, 100)
		<-d.Context().Done()
	})
	client := stack.connect(t, dialerUser, dialerWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-ring"})

	ringback := voip.NewToneSource(voip.BrazilRingback)
	for heard := 0; heard < 10; {
		message := client.next()
		if message.Type == WSEventCallStatus && callStatusOf(message) != "ringing" {
			t.Fatalf("status %q while ringing: alerting must stay inside the server", callStatusOf(message))
		}
		if message.Type != WSEventCallAudioS {
			continue
		}
		if frame := audioOf(t, message); !bytes.Equal(frame, ringback.Next()) {
			t.Fatalf("frame %d is not the 425 Hz E.180 ringback", heard)
		}
		heard++
	}

	close(answer)
	client.waitFor(WSEventCallStatus, statusIs("answered"))
	carrierSeen := false
	for frames := 0; frames < 30; {
		message := client.next()
		if message.Type != WSEventCallAudioS {
			continue
		}
		frames++
		tone := isTone(audioOf(t, message))
		if !tone {
			carrierSeen = true
		}
		if carrierSeen && tone {
			t.Fatal("ringback kept playing after the answer")
		}
	}
	if !carrierSeen {
		t.Fatal("the answered call's audio never replaced the ringback")
	}
	client.send(WSEventEndCall, map[string]string{"request_id": "req-ring"})
	client.waitFor(WSEventCallEnded, nil)
}

func TestTheCarriersEarlyMediaReplacesOurRingback(t *testing.T) {
	stack := startDialerStack(t)
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		time.Sleep(300 * time.Millisecond)
		if err := d.ProgressMedia(); err != nil {
			return
		}
		voiptest.SendRTP(d, 40)
		if err := d.Answer(); err != nil {
			return
		}
		<-d.Context().Done()
	})
	client := stack.connect(t, dialerUser, dialerWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-early"})

	ringbackHeard, announcementHeard := false, false
	for {
		message := client.next()
		if message.Type == WSEventCallStatus && callStatusOf(message) == "answered" {
			break
		}
		if message.Type != WSEventCallAudioS {
			continue
		}
		tone := isTone(audioOf(t, message))
		if tone && announcementHeard {
			t.Fatal("ringback played over the carrier's announcement")
		}
		ringbackHeard = ringbackHeard || tone
		announcementHeard = announcementHeard || !tone
	}
	if !ringbackHeard {
		t.Fatal("no ringback before the carrier started its announcement")
	}
	if !announcementHeard {
		t.Fatal("the carrier's early media never reached the operator")
	}
	if len(stack.manager.ActiveCalls(stack.trunk.ID)) != 1 {
		t.Fatal("the call answered after early media is not tracked as answered")
	}
	client.send(WSEventEndCall, map[string]string{"request_id": "req-early"})
	client.waitFor(WSEventCallEnded, nil)
}

func TestAFailedCallSoundsCongestionBeforeItEnds(t *testing.T) {
	stack := startDialerStack(t)
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Respond(sip.StatusServiceUnavailable, "Service Unavailable", nil)
	})
	client := stack.connect(t, dialerUser, dialerWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-failed"})

	congestion := voip.NewToneSource(voip.BrazilCongestion)
	var heard [][]byte
	for {
		message := client.next()
		if message.Type == WSEventCallAudioS {
			heard = append(heard, audioOf(t, message))
		}
		if message.Type == WSEventCallEnded {
			break
		}
	}
	if len(heard) < 50 {
		t.Fatalf("heard %d frames, want about a second of congestion tone", len(heard))
	}
	for i, frame := range heard[:50] {
		if !bytes.Equal(frame, congestion.Next()) {
			t.Fatalf("frame %d is not the E.180 congestion cadence", i)
		}
	}
}

func TestABusyNumberSoundsBusyBeforeTheCallEnds(t *testing.T) {
	stack := startDialerStack(t)
	stack.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		time.Sleep(200 * time.Millisecond)
		_ = d.Respond(sip.StatusBusyHere, "Busy Here", nil)
	})
	client := stack.connect(t, dialerUser, dialerWorkspace)
	client.send(WSEventStartCall, StartCallPayload{PhoneNumber: "100", TrunkID: stack.trunk.ID, RequestID: "req-busy"})

	busy := voip.NewToneSource(voip.BrazilBusy).Next()
	busyHeard := false
	for {
		message := client.next()
		if message.Type == WSEventCallAudioS && bytes.Equal(audioOf(t, message), busy) {
			busyHeard = true
		}
		if message.Type == WSEventCallEnded {
			var ended CallEndedPayload
			_ = json.Unmarshal(message.Payload, &ended)
			if ended.Reason != "busy" {
				t.Fatalf("ended reason = %q, want busy", ended.Reason)
			}
			break
		}
	}
	if !busyHeard {
		t.Fatal("the operator never heard the busy tone")
	}
	if len(stack.manager.ActiveCalls(stack.trunk.ID)) != 0 {
		t.Fatal("the engine still tracks the rejected call")
	}
}
