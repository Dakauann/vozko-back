package sip_trunk_usecase

import (
	"testing"

	"vozko/domain/sip_trunk"
	"vozko/domain/sip_trunk/siptrunktest"
)

func TestOnlyTrunksOfTheWorkspaceThatTakeCallsCanRunVoiceWorkflows(t *testing.T) {
	outboundOnly := trunkNamed("outbound", "Saída")
	outboundOnly.TrunkType = sip_trunk.TrunkTypeOutbound
	trunks := NewInboundTrunks(siptrunktest.NewMemoryRepository(trunkNamed("both", "Principal"), outboundOnly))

	cases := []struct {
		workspace, trunk string
		want             bool
	}{
		{ownerWorkspace, "both", true},
		{ownerWorkspace, "outbound", false},
		{strangerWorkspace, "both", false},
		{ownerWorkspace, "missing", false},
	}
	for _, tc := range cases {
		got, err := trunks.ReceivesCalls(tc.workspace, tc.trunk)
		if err != nil || got != tc.want {
			t.Errorf("%s/%s = %v, %v; want %v", tc.workspace, tc.trunk, got, err, tc.want)
		}
	}
}
