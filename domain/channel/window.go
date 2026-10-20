package channel

import "time"

type WindowTier string

const (
	WindowTierNone     WindowTier = ""
	WindowTierStandard WindowTier = "standard"
	WindowTierHuman    WindowTier = "human_agent"
)

func (c Capabilities) Window(lastInbound *time.Time, now time.Time, humanInitiated bool) (WindowTier, *time.Time) {
	if c.OutboundWindow <= 0 {
		return WindowTierStandard, nil
	}
	if lastInbound == nil {
		return WindowTierNone, nil
	}
	standard := lastInbound.Add(c.OutboundWindow)
	if now.Before(standard) {
		return WindowTierStandard, &standard
	}
	if !c.HumanAgentWindow || !humanInitiated || c.ExtendedWindow <= c.OutboundWindow {
		return WindowTierNone, nil
	}
	extended := lastInbound.Add(c.ExtendedWindow)
	if now.Before(extended) {
		return WindowTierHuman, &extended
	}
	return WindowTierNone, nil
}

func (c Capabilities) ReplyHorizon(humanInitiated bool) time.Duration {
	if c.OutboundWindow <= 0 {
		return 0
	}
	if c.HumanAgentWindow && humanInitiated && c.ExtendedWindow > c.OutboundWindow {
		return c.ExtendedWindow
	}
	return c.OutboundWindow
}
