package loopguard

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/cache"
)

type Decision struct {
	Block bool

	Reason string

	Count int64
}

const (
	ReasonDuplicateInbound = "duplicate_inbound"
	ReasonAIRateLimit      = "ai_rate_limit"
)

type Guard interface {
	CheckInbound(ctx context.Context, workspaceID, conversationID, text string) Decision

	RecordAIResponse(ctx context.Context, workspaceID, conversationID string) Decision
}

type Config struct {
	DuplicateWindow time.Duration

	DuplicateThreshold int64

	AIRateWindow time.Duration

	AIRateMax int64

	KeyPrefix string
}

func DefaultConfig() Config {
	return Config{
		DuplicateWindow:    10 * time.Minute,
		DuplicateThreshold: 3,

		AIRateWindow: 10 * time.Minute,
		AIRateMax:    30,
		KeyPrefix:    "loopguard",
	}
}

func (c Config) resolve() Config {
	d := DefaultConfig()
	if c.DuplicateWindow <= 0 {
		c.DuplicateWindow = d.DuplicateWindow
	}
	if c.DuplicateThreshold <= 0 {
		c.DuplicateThreshold = d.DuplicateThreshold
	}
	if c.AIRateWindow <= 0 {
		c.AIRateWindow = d.AIRateWindow
	}
	if c.AIRateMax <= 0 {
		c.AIRateMax = d.AIRateMax
	}
	if strings.TrimSpace(c.KeyPrefix) == "" {
		c.KeyPrefix = d.KeyPrefix
	}
	return c
}

type MetricsRecorder interface {
	LoopGuardChecked(layer, action string)
	LoopGuardBlocked(reason string)
}

type NoopMetrics struct{}

func (NoopMetrics) LoopGuardChecked(string, string) {}
func (NoopMetrics) LoopGuardBlocked(string)         {}

type guard struct {
	state   cache.SharedState
	cfg     Config
	metrics MetricsRecorder
}

func NewGuard(state cache.SharedState, cfg Config, metrics MetricsRecorder) Guard {
	if state == nil {
		return AlwaysAllow{}
	}
	if metrics == nil {
		metrics = NoopMetrics{}
	}
	return &guard{
		state:   state,
		cfg:     cfg.resolve(),
		metrics: metrics,
	}
}

type AlwaysAllow struct{}

func (AlwaysAllow) CheckInbound(context.Context, string, string, string) Decision {
	return Decision{}
}
func (AlwaysAllow) RecordAIResponse(context.Context, string, string) Decision {
	return Decision{}
}

func (g *guard) CheckInbound(_ context.Context, workspaceID, conversationID, text string) Decision {

	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return Decision{}
	}
	fp := Fingerprint(text)
	if fp == "" {

		return Decision{}
	}

	key := g.cfg.KeyPrefix + ":dup:" + safeLabel(workspaceID) + ":" + conversationID + ":" + fp
	count, err := g.state.IncrWithTTL(key, g.cfg.DuplicateWindow)
	if err != nil || count <= 0 {

		if err != nil {
			log.Printf("[loopguard] inbound check backend error (workspace=%s conv=%s): %v", workspaceID, conversationID, err)
		}
		return Decision{}
	}

	if count >= g.cfg.DuplicateThreshold {
		g.metrics.LoopGuardChecked("inbound", "block")
		g.metrics.LoopGuardBlocked(ReasonDuplicateInbound)
		return Decision{Block: true, Reason: ReasonDuplicateInbound, Count: count}
	}

	g.metrics.LoopGuardChecked("inbound", "pass")
	return Decision{Count: count}
}

func (g *guard) RecordAIResponse(_ context.Context, workspaceID, conversationID string) Decision {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return Decision{}
	}
	key := g.cfg.KeyPrefix + ":airate:" + safeLabel(workspaceID) + ":" + conversationID
	count, err := g.state.IncrWithTTL(key, g.cfg.AIRateWindow)
	if err != nil || count <= 0 {
		if err != nil {
			log.Printf("[loopguard] airate increment backend error (workspace=%s conv=%s): %v", workspaceID, conversationID, err)
		}
		return Decision{}
	}

	if count > g.cfg.AIRateMax {
		g.metrics.LoopGuardChecked("airate", "block")
		g.metrics.LoopGuardBlocked(ReasonAIRateLimit)
		return Decision{Block: true, Reason: ReasonAIRateLimit, Count: count}
	}

	g.metrics.LoopGuardChecked("airate", "pass")
	return Decision{Count: count}
}

func safeLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "_"
	}
	return s
}

var ErrNilState = errors.New("loopguard: shared state is nil")
