package unofficial_whatsapp

import "testing"

func TestANumberIsReachableOnlyWithAChatToWrite(t *testing.T) {
	cases := []struct {
		check NumberCheck
		want  bool
	}{
		{NumberCheck{IsOnWhatsApp: true, JID: "5511981124410@s.whatsapp.net"}, true},
		{NumberCheck{IsOnWhatsApp: true, JID: "  "}, false},
		{NumberCheck{IsOnWhatsApp: false, JID: "5511981124410@s.whatsapp.net"}, false},
	}
	for _, c := range cases {
		if got := c.check.Reachable(); got != c.want {
			t.Errorf("%+v reachable = %v, want %v", c.check, got, c.want)
		}
	}
}
