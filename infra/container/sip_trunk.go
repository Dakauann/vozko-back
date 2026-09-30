package container

import (
	"context"
	"log"
	"time"

	"vozko/brand"
	siptrunkhttp "vozko/delivery/http/siptrunk"
	"vozko/domain/sip_trunk"
	workspace_domain "vozko/domain/workspace"
	sip_trunk_repository "vozko/infra/repositories/sip_trunk"
	voipinfra "vozko/infra/voip"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
	workspace_usecase "vozko/usecases/workspace"
)

const sipWatchInterval = 5 * time.Second

type sipTrunkBundle struct {
	Repository  sip_trunk.Repository
	Engine      *voipinfra.SIPTrunkManager
	Handler     *siptrunkhttp.Handler
	Planner     sip_trunk.CallPlanner
	CallSource  *sip_trunk_usecase.CallSource
	Permissions trunkCallPermissions
}

func (c *Container) initSIPTrunks() {
	cfg := c.cfg.SIP
	repo := sip_trunk_repository.NewRepository(c.db)
	engine, err := voipinfra.NewSIPTrunkManager(voipinfra.TrunkManagerConfig{
		SIPBindHost:     cfg.BindHost,
		SIPPortStart:    cfg.PortStart,
		SIPPortCount:    cfg.PortCount,
		RTPPortStart:    cfg.RTPPortStart,
		RTPPortEnd:      cfg.RTPPortEnd,
		RegisterExpiry:  cfg.RegisterExpiry,
		DialTimeout:     cfg.DialTimeout,
		MediaTimeout:    cfg.MediaTimeout,
		MaxCallDuration: cfg.MaxCallDuration,
		WatchInterval:   sipWatchInterval,
		PublicAddress:   cfg.PublicAddress,
		STUNServers:     cfg.STUNServers,
		UserAgent:       brand.Active().Name,
		Debug:           cfg.Debug,
		CallMetrics:     c.services.metrics,

		AllowPrivateHosts: cfg.AllowPrivateHosts,
	}, repo)
	if err != nil {
		log.Fatalf("Failed to build the SIP trunk engine: %v", err)
	}
	permissions := trunkCallPermissions{access: workspace_usecase.NewCheckAccessUseCase(c.repositories.workspace)}
	planner := sip_trunk_usecase.NewCallPlanner(repo, engine, permissions)
	c.sipTrunks = &sipTrunkBundle{
		Permissions: permissions,
		Repository:  repo,
		Engine:      engine,
		Planner:     planner,
		CallSource:  sip_trunk_usecase.NewCallSource(planner, engine),
		Handler: siptrunkhttp.NewHandler(siptrunkhttp.HandlerDeps{
			Create:    sip_trunk_usecase.NewCreateTrunkUseCase(repo, engine),
			Update:    sip_trunk_usecase.NewUpdateTrunkUseCase(repo, engine),
			Delete:    sip_trunk_usecase.NewDeleteTrunkUseCase(repo, engine),
			List:      sip_trunk_usecase.NewListTrunksUseCase(repo, engine),
			Get:       sip_trunk_usecase.NewGetTrunkUseCase(repo, engine),
			Hangup:    sip_trunk_usecase.NewHangupCallUseCase(repo, engine),
			ListCalls: sip_trunk_usecase.NewListCallsUseCase(repo, engine),
		}),
	}
}

func (c *Container) startSIPTrunks() {
	if err := c.sipTrunks.Engine.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start the SIP trunk engine: %v", err)
	}
}

func (c *Container) stopSIPTrunks() {
	if c.sipTrunks == nil {
		return
	}
	if err := c.sipTrunks.Engine.Stop(); err != nil {
		log.Printf("SIP trunk engine shutdown: %v", err)
	}
}

func sipTrunkHandler(c *Container) *siptrunkhttp.Handler {
	if c.sipTrunks == nil {
		return nil
	}
	return c.sipTrunks.Handler
}

type trunkCallPermissions struct {
	access workspace_domain.CheckAccessUseCase
}

func (p trunkCallPermissions) MayCallThroughTrunks(userID, workspaceID string, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	return p.access.Execute(userID, workspaceID, workspace_domain.ResourceSIPTrunks, workspace_domain.ActionCall) == nil
}
