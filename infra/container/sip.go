package container

import (
	"context"
	"log"
	"time"

	"vozko/domain/cluster"
	sip_trunk_domain "vozko/domain/sip_trunk"
	voipinfra "vozko/infra/voip"
)

func (c *Container) initSIPTrunkManager() {
	ownership := sip_trunk_domain.NewTrunkOwnershipManager(
		c.redisProvider.SharedState(),
		c.replicaID,
	)

	const replicaHeartbeatTTL = 30 * time.Second
	_ = c.redisProvider.SharedState().SetString(
		cluster.HeartbeatKeyPrefix+c.replicaID,
		"1",
		replicaHeartbeatTTL,
	)
	replicas := cluster.NewRegistry(c.redisProvider.SharedState())
	if err := replicas.Announce(c.replicaID); err != nil {
		log.Printf("Warning: failed to announce replica %s in cluster registry: %v", c.replicaID, err)
	}

	cfg := voipinfra.TrunkManagerConfig{
		SIPPortStart:        c.cfg.SIPTrunkPortStart,
		SIPPortCount:        c.cfg.SIPTrunkPortCount,
		RTPPortStart:        c.cfg.SIPTrunkRTPPortStart,
		RTPPortEnd:          c.cfg.SIPTrunkRTPPortEnd,
		RegisterInterval:    25 * time.Second,
		RegistrationTimeout: 30 * time.Second,
		Metrics:             c.services.metrics,
		CallMetrics:         c.services.metrics,
		Ownership:           ownership,
		Replicas:            replicas,
	}

	manager, err := voipinfra.NewSIPTrunkManager(cfg, c.repositories.sipTrunk)
	if err != nil {
		log.Printf("Warning: Failed to create SIP trunk manager: %v", err)
		return
	}

	if err := manager.Start(context.Background()); err != nil {
		log.Printf("Warning: Failed to start SIP trunk manager: %v", err)
		return
	}

	c.services.sipTrunkManager = manager
	c.services.trunkOwnership = ownership
	c.services.clusterRegistry = replicas
	log.Printf("SIP trunk manager started (replica=%s, SIP ports %d-%d, RTP ports %d-%d)",
		c.replicaID,
		c.cfg.SIPTrunkPortStart,
		c.cfg.SIPTrunkPortStart+c.cfg.SIPTrunkPortCount-1,
		c.cfg.SIPTrunkRTPPortStart,
		c.cfg.SIPTrunkRTPPortEnd,
	)
}
