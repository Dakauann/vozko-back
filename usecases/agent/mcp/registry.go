package mcp

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/remote"
	"vozko/infra/agent/mcp/vault"
)

type SourceBuilder interface {
	Build(server *domainmcp.RemoteMCPServer, secret string) domainmcp.ToolSource
}

type DefaultSourceBuilder struct{}

func (DefaultSourceBuilder) Build(server *domainmcp.RemoteMCPServer, secret string) domainmcp.ToolSource {
	return remote.New(server, secret)
}

type cachedSource struct {
	source    domainmcp.ToolSource
	createdAt time.Time
}

const defaultSourceCacheTTL = 5 * time.Minute

type BuiltinCatalog interface {
	Descriptor(key string) (domainmcp.BuiltinDescriptor, bool)
	All() []domainmcp.BuiltinDescriptor
}

type StaticCatalog struct {
	entries map[string]domainmcp.BuiltinDescriptor
	order   []string
}

func NewStaticCatalog(descs ...domainmcp.BuiltinDescriptor) *StaticCatalog {
	m := make(map[string]domainmcp.BuiltinDescriptor, len(descs))
	order := make([]string, 0, len(descs))
	for _, d := range descs {
		m[d.Key] = d
		order = append(order, d.Key)
	}
	sort.Strings(order)
	return &StaticCatalog{entries: m, order: order}
}

func (s *StaticCatalog) Descriptor(key string) (domainmcp.BuiltinDescriptor, bool) {
	d, ok := s.entries[key]
	return d, ok
}

func (s *StaticCatalog) All() []domainmcp.BuiltinDescriptor {
	out := make([]domainmcp.BuiltinDescriptor, 0, len(s.order))
	for _, k := range s.order {
		out = append(out, s.entries[k])
	}
	return out
}

type Registry struct {
	Builtins BuiltinCatalog
	Bindings domainmcp.BuiltinBindingRepository
	Remotes  domainmcp.RemoteServerRepository
	Cache    domainmcp.ToolCacheRepository
	Vault    *vault.Vault
	Builder  SourceBuilder

	Refresh *RefreshOAuth2UseCase

	sourceCache sync.Map

	cacheTTL time.Duration
}

func NewRegistry(
	builtins BuiltinCatalog,
	bindings domainmcp.BuiltinBindingRepository,
	remotes domainmcp.RemoteServerRepository,
	cache domainmcp.ToolCacheRepository,
	v *vault.Vault,
) *Registry {
	return &Registry{
		Builtins: builtins,
		Bindings: bindings,
		Remotes:  remotes,
		Cache:    cache,
		Vault:    v,
		Builder:  DefaultSourceBuilder{},
		cacheTTL: defaultSourceCacheTTL,
	}
}

func (r *Registry) Sources(ctx context.Context, ws domainmcp.WorkspaceID) ([]domainmcp.ToolSource, error) {
	if ws.Empty() {
		return nil, domainmcp.ErrWorkspaceRequired
	}
	bindings, err := r.Bindings.ListByWorkspace(ctx, ws.String())
	if err != nil {
		return nil, err
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ServerKey < bindings[j].ServerKey })
	remotes, err := r.Remotes.ListByWorkspace(ctx, ws.String())
	if err != nil {
		return nil, err
	}
	sort.Slice(remotes, func(i, j int) bool { return remotes[i].Name < remotes[j].Name })

	out := make([]domainmcp.ToolSource, 0, len(bindings)+len(remotes))
	for _, b := range bindings {
		if b.Status != domainmcp.StatusConnected {
			continue
		}
		desc, ok := r.Builtins.Descriptor(b.ServerKey)
		if !ok {
			continue
		}
		var cred *domainmcp.Credential
		if b.Credential != nil {
			plain, err := r.decryptIfAny(b.Credential)
			if err != nil {
				return nil, err
			}
			cred = &domainmcp.Credential{
				Mode:        b.Credential.Mode,
				Cipher:      []byte(plain),
				KEKVersion:  b.Credential.KEKVersion,
				ExpiresAt:   b.Credential.ExpiresAt,
				RefreshHint: b.Credential.RefreshHint,
			}
		}
		out = append(out, desc.Builder(cred))
	}
	for _, s := range remotes {
		if s.Status != domainmcp.StatusConnected {
			continue
		}

		if r.Refresh != nil && s.Credential.ShouldRefresh(domainmcp.Now()) {
			if _, refErr := r.Refresh.RefreshRemote(ctx, ws.String(), s.ID); refErr != nil {
				if s.Credential.Expired(domainmcp.Now()) {
					log.Printf("[MCP] OAuth2 refresh failed for source remote:%s (token expired): %v", s.ID, refErr)
					r.sourceCache.Delete(fmt.Sprintf("%s:remote:%s", ws, s.ID))
					continue
				}
				log.Printf("[MCP] OAuth2 refresh failed for source remote:%s (using cached token): %v", s.ID, refErr)
			} else {
				reloaded, reloadErr := r.Remotes.Get(ctx, ws.String(), s.ID)
				if reloadErr == nil && reloaded != nil {
					s = reloaded
				}

				r.sourceCache.Delete(fmt.Sprintf("%s:remote:%s", ws, s.ID))
			}
		}

		cacheKey := fmt.Sprintf("%s:remote:%s", ws, s.ID)
		if cached, ok := r.sourceCache.Load(cacheKey); ok {
			if cs := cached.(*cachedSource); time.Since(cs.createdAt) < r.cacheTTL {
				out = append(out, cs.source)
				continue
			}
			r.sourceCache.Delete(cacheKey)
		}

		secret, err := r.decryptIfAny(s.Credential)
		if err != nil {
			return nil, err
		}
		src := r.Builder.Build(s, secret)
		if rs, ok := src.(*remote.Source); ok {
			server := s
			rs.WithAuthFailureCallback(func() {
				r.sourceCache.Delete(cacheKey)
				server.Status = domainmcp.StatusDisconnected
				if updateErr := r.Remotes.Update(ctx, server); updateErr != nil {
					log.Printf("[MCP] failed to update status for source %s: %v", src.ID(), updateErr)
				} else {
					log.Printf("[MCP] marked source %s as status=disconnected due to auth failure", src.ID())
				}
			})
		}
		r.sourceCache.Store(cacheKey, &cachedSource{source: src, createdAt: time.Now()})
		out = append(out, src)
	}
	return out, nil
}

func (r *Registry) SourcesForCollections(
	ctx context.Context,
	ws domainmcp.WorkspaceID,
	collections domainmcp.CollectionRepository,
	collectionIDs []string,
) ([]domainmcp.ToolSource, error) {
	if ws.Empty() {
		return nil, domainmcp.ErrWorkspaceRequired
	}
	if len(collectionIDs) == 0 {
		return []domainmcp.ToolSource{}, nil
	}
	if collections == nil {
		return []domainmcp.ToolSource{}, nil
	}
	cs, err := collections.ListByIDs(ctx, ws.String(), collectionIDs)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]struct{})
	for _, c := range cs {
		for _, m := range c.Members {
			allow[string(m.Kind)+":"+m.RefID] = struct{}{}
		}
	}
	if len(allow) == 0 {
		return []domainmcp.ToolSource{}, nil
	}
	all, err := r.Sources(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := make([]domainmcp.ToolSource, 0, len(all))
	for _, s := range all {
		if _, ok := allow[s.ID()]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (r *Registry) Resolve(ctx context.Context, ws domainmcp.WorkspaceID, qualified string) (domainmcp.ToolSource, string, error) {
	q, err := domainmcp.ParseQualifiedName(qualified)
	if err != nil {
		return nil, "", err
	}
	sources, err := r.Sources(ctx, ws)
	if err != nil {
		return nil, "", err
	}
	needle := string(q.Kind) + ":" + q.SourceID
	for _, s := range sources {
		if s.ID() == needle {
			return s, q.Tool, nil
		}
	}
	return nil, "", domainmcp.ErrToolNotFound
}

func (r *Registry) InvalidateSource(wsID domainmcp.WorkspaceID, sourceID string) {
	r.sourceCache.Delete(fmt.Sprintf("%s:%s", wsID, sourceID))
}

func (r *Registry) decryptIfAny(cred *domainmcp.Credential) (string, error) {
	if cred == nil || cred.Mode == domainmcp.AuthNone || len(cred.Cipher) == 0 {
		return "", nil
	}
	plain, err := r.Vault.Open(cred.Cipher)
	if err != nil {
		return "", err
	}

	if cred.Mode == domainmcp.AuthOAuth2 {
		return domainmcp.DecodeOAuth2Secret(plain).AccessToken, nil
	}
	return string(plain), nil
}
