package container

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"vozko/delivery/http/handlers"
	domainmcp "vozko/domain/agent/mcp"
	mcpoauth "vozko/infra/agent/mcp/oauth"
	mcpvault "vozko/infra/agent/mcp/vault"
	mcprepo "vozko/infra/repositories/agent/mcp/gorm"
	ucmcp "vozko/usecases/agent/mcp"
)

type agentMCPDeps struct {
	Catalog     ucmcp.BuiltinCatalog
	Bindings    domainmcp.BuiltinBindingRepository
	Remotes     domainmcp.RemoteServerRepository
	ToolCache   domainmcp.ToolCacheRepository
	Collections domainmcp.CollectionRepository
	Vault       *mcpvault.Vault
	Signer      *mcpoauth.Signer
}

func (c *Container) initAgentMCP() *handlers.AgentMCPBundle {
	deps := c.buildAgentMCPDeps()
	c.mcpCollection = deps.Collections
	c.mcpRegistry = ucmcp.NewRegistry(deps.Catalog, deps.Bindings, deps.Remotes, deps.ToolCache, deps.Vault)

	enable := &ucmcp.EnableBuiltinUseCase{
		Catalog:  deps.Catalog,
		Bindings: deps.Bindings,
		Vault:    deps.Vault,
	}
	configure := &ucmcp.ConfigureBuiltinAuthUseCase{
		Catalog:  deps.Catalog,
		Bindings: deps.Bindings,
		Vault:    deps.Vault,
	}
	register := &ucmcp.RegisterRemoteUseCase{
		Remotes: deps.Remotes,
		Cache:   deps.ToolCache,
		Vault:   deps.Vault,
		Prober:  ucmcp.ClientProber{},
	}
	builtinResolver := &ucmcp.EnvResolver{Catalog: deps.Catalog, Bindings: deps.Bindings, RedirectURL: c.cfg.MCPCallbackURL}
	remoteResolver := &ucmcp.RemoteOAuthResolver{Remotes: deps.Remotes, Vault: deps.Vault, RedirectURL: c.cfg.MCPCallbackURL}
	chainResolver := &ucmcp.ChainResolver{Builtin: builtinResolver, Remote: remoteResolver}
	startOAuth := &ucmcp.StartOAuth2UseCase{
		Resolver: chainResolver,
		Signer:   deps.Signer,
	}
	register.OAuth = ucmcp.HTTPOAuthDiscoverer{HTTP: &http.Client{Timeout: 15 * time.Second}}
	register.StartOAuth = startOAuth
	register.RedirectURL = c.cfg.MCPCallbackURL
	completeOAuth := &ucmcp.CompleteOAuth2UseCase{
		Resolver: chainResolver,
		Signer:   deps.Signer,
		Bindings: deps.Bindings,
		Remotes:  deps.Remotes,
		Cache:    deps.ToolCache,
		Prober:   ucmcp.ClientProber{},
		Vault:    deps.Vault,
		HTTP:     &http.Client{Timeout: 20 * time.Second},
	}
	refreshOAuth := &ucmcp.RefreshOAuth2UseCase{
		Resolver: chainResolver,
		Remotes:  deps.Remotes,
		Bindings: deps.Bindings,
		Vault:    deps.Vault,
		HTTP:     &http.Client{Timeout: 20 * time.Second},
	}

	c.mcpRegistry.Refresh = refreshOAuth

	remoteHandler := handlers.NewAgentMCPRemoteHandler(register, startOAuth, deps.Remotes, deps.ToolCache)
	remoteHandler.Refresh = refreshOAuth

	return &handlers.AgentMCPBundle{
		Builtin: handlers.NewAgentMCPHandler(deps.Catalog, enable, configure, deps.Bindings, startOAuth),
		Remote:  remoteHandler,

		OAuthCallback: handlers.NewAgentMCPOAuthCallbackHandler(completeOAuth, ""),
		Collection: handlers.NewAgentMCPCollectionHandler(
			ucmcp.NewCollectionService(deps.Collections, deps.Bindings, deps.Remotes, deps.Catalog, nil),
		),
	}
}

func (c *Container) buildAgentMCPDeps() agentMCPDeps {

	catalog := ucmcp.NewStaticCatalog()

	vaultKey := mcpKeyFromConfig(c.cfg.MCPVaultKey, c.cfg.AuthJWTSecret, "vozko-mcp-vault-v1")
	vault, err := mcpvault.New(vaultKey, c.cfg.MCPVaultKEKVersion)
	if err != nil {
		log.Fatalf("mcp vault: %v", err)
	}

	signerKey := mcpKeyFromConfig(c.cfg.MCPStateSignerKey, c.cfg.AuthJWTSecret, "vozko-mcp-signer-v1")
	signer := mcpoauth.NewSigner(signerKey, 10*time.Minute)

	return agentMCPDeps{
		Catalog:     catalog,
		Bindings:    mcprepo.NewBuiltinBindingRepo(c.db),
		Remotes:     mcprepo.NewRemoteServerRepo(c.db),
		ToolCache:   mcprepo.NewToolCacheRepo(c.db),
		Collections: mcprepo.NewCollectionRepo(c.db),
		Vault:       vault,
		Signer:      signer,
	}
}

func mcpKeyFromConfig(hexKey, fallbackSecret, purpose string) []byte {
	hexKey = strings.TrimSpace(hexKey)
	if hexKey != "" {
		b, err := hex.DecodeString(hexKey)
		if err != nil {
			log.Fatalf("mcp: %s must be hex-encoded: %v", purpose, err)
		}
		if len(b) != 32 {
			log.Fatalf("mcp: %s must decode to 32 bytes (got %d)", purpose, len(b))
		}
		return b
	}
	sum := sha256.Sum256([]byte(purpose + "|" + fallbackSecret))
	return sum[:]
}
