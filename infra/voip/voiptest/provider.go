package voiptest

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
)

const (
	Username = "1001"
	Password = "s3cret"
)

type CallBehaviour func(d *diago.DialogServerSession)

type Provider struct {
	Port          int
	Dialer        *diago.Diago
	InvitedUsers  chan string
	registrations atomic.Int32
	behaviour     atomic.Pointer[CallBehaviour]
}

func FreeUDPPort(t testing.TB) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func StartProvider(t testing.TB) *Provider {
	t.Helper()
	p := &Provider{Port: FreeUDPPort(t), InvitedUsers: make(chan string, 16)}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("FakeProvider"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	digest := diago.NewDigestServer()
	auth := diago.DigestAuth{Username: Username, Password: Password, Realm: "fake"}
	server.OnRegister(func(req *sip.Request, tx sip.ServerTransaction) {
		res, err := digest.AuthorizeRequest(req, auth)
		if err == nil && res.StatusCode == sip.StatusOK {
			p.registrations.Add(1)
			res.AppendHeader(sip.NewHeader("Expires", "60"))
		}
		_ = tx.Respond(res)
	})
	p.Dialer = diago.NewDiago(ua, diago.WithServer(server), diago.WithTransport(diago.Transport{
		Transport: "udp4",
		BindHost:  "127.0.0.1",
		BindPort:  p.Port,
	}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		digest.Close()
		_ = ua.Close()
	})
	if err := p.Dialer.ServeBackground(ctx, func(d *diago.DialogServerSession) {
		if err := digest.AuthorizeDialog(d, auth); err != nil {
			return
		}
		p.InvitedUsers <- d.ToUser()
		if behaviour := p.behaviour.Load(); behaviour != nil {
			(*behaviour)(d)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *Provider) OnCall(b CallBehaviour) {
	p.behaviour.Store(&b)
}

func (p *Provider) Registrations() int {
	return int(p.registrations.Load())
}

type mediaDialog interface {
	MediaSession() *media.MediaSession
}

func SendRTP(d mediaDialog, count int) {
	session := d.MediaSession()
	for seq := 1; seq <= count; seq++ {
		packet := &rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: 0, SequenceNumber: uint16(seq), Timestamp: uint32(seq) * 160, SSRC: 7},
			Payload: make([]byte, 160),
		}
		if err := session.WriteRTP(packet); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func AnswerAndHold(ended chan<- struct{}) CallBehaviour {
	return func(d *diago.DialogServerSession) {
		if ended != nil {
			defer close(ended)
		}
		if err := d.Answer(); err != nil {
			return
		}
		<-d.Context().Done()
	}
}
