package memory

import (
	"context"
	"sync"

	"vozko/domain/agent/mcp"
)

type BuiltinBindingRepo struct {
	mu   sync.Mutex
	rows map[string]map[string]*mcp.BuiltinBinding
}

func NewBuiltinBindingRepo() *BuiltinBindingRepo {
	return &BuiltinBindingRepo{rows: map[string]map[string]*mcp.BuiltinBinding{}}
}

func (r *BuiltinBindingRepo) Upsert(_ context.Context, b *mcp.BuiltinBinding) error {
	if b == nil || b.WorkspaceID == "" {
		return mcp.ErrWorkspaceRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ws, ok := r.rows[b.WorkspaceID]
	if !ok {
		ws = map[string]*mcp.BuiltinBinding{}
		r.rows[b.WorkspaceID] = ws
	}
	cp := *b
	ws[b.ID] = &cp
	return nil
}

func (r *BuiltinBindingRepo) GetByID(_ context.Context, ws, id string) (*mcp.BuiltinBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.rows[ws]; ok {
		if b, ok := m[id]; ok {
			cp := *b
			return &cp, nil
		}
	}
	return nil, mcp.ErrBindingNotFound
}

func (r *BuiltinBindingRepo) ListByWorkspace(_ context.Context, ws string) ([]*mcp.BuiltinBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.rows[ws]
	out := make([]*mcp.BuiltinBinding, 0, len(m))
	for _, b := range m {
		cp := *b
		out = append(out, &cp)
	}
	return out, nil
}

func (r *BuiltinBindingRepo) Delete(_ context.Context, ws, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.rows[ws]; ok {
		if _, ok := m[id]; !ok {
			return mcp.ErrBindingNotFound
		}
		delete(m, id)
		return nil
	}
	return mcp.ErrBindingNotFound
}

type RemoteServerRepo struct {
	mu   sync.Mutex
	rows map[string]map[string]*mcp.RemoteMCPServer
}

func NewRemoteServerRepo() *RemoteServerRepo {
	return &RemoteServerRepo{rows: map[string]map[string]*mcp.RemoteMCPServer{}}
}

func (r *RemoteServerRepo) Create(_ context.Context, s *mcp.RemoteMCPServer) error {
	if s == nil || s.WorkspaceID == "" {
		return mcp.ErrWorkspaceRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.rows[s.WorkspaceID]
	if !ok {
		m = map[string]*mcp.RemoteMCPServer{}
		r.rows[s.WorkspaceID] = m
	}
	for _, existing := range m {
		if existing.URL == s.URL {
			return mcp.ErrDuplicate
		}
	}
	cp := *s
	m[s.ID] = &cp
	return nil
}

func (r *RemoteServerRepo) Update(_ context.Context, s *mcp.RemoteMCPServer) error {
	if s == nil || s.WorkspaceID == "" {
		return mcp.ErrWorkspaceRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.rows[s.WorkspaceID]
	if !ok {
		return mcp.ErrRemoteServerNotFound
	}
	if _, ok := m[s.ID]; !ok {
		return mcp.ErrRemoteServerNotFound
	}
	cp := *s
	m[s.ID] = &cp
	return nil
}

func (r *RemoteServerRepo) Get(_ context.Context, ws, id string) (*mcp.RemoteMCPServer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.rows[ws]; ok {
		if s, ok := m[id]; ok {
			cp := *s
			return &cp, nil
		}
	}
	return nil, mcp.ErrRemoteServerNotFound
}

func (r *RemoteServerRepo) ListByWorkspace(_ context.Context, ws string) ([]*mcp.RemoteMCPServer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.rows[ws]
	out := make([]*mcp.RemoteMCPServer, 0, len(m))
	for _, s := range m {
		cp := *s
		out = append(out, &cp)
	}
	return out, nil
}

func (r *RemoteServerRepo) Delete(_ context.Context, ws, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.rows[ws]; ok {
		if _, ok := m[id]; !ok {
			return mcp.ErrRemoteServerNotFound
		}
		delete(m, id)
		return nil
	}
	return mcp.ErrRemoteServerNotFound
}

type ToolCacheRepo struct {
	mu   sync.Mutex
	rows map[string][]mcp.CachedTool
}

func NewToolCacheRepo() *ToolCacheRepo {
	return &ToolCacheRepo{rows: map[string][]mcp.CachedTool{}}
}

func cacheKey(source, ws string) string { return source + "|" + ws }

func (r *ToolCacheRepo) Replace(_ context.Context, sourceID, ws string, tools []mcp.CachedTool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]mcp.CachedTool, len(tools))
	copy(cp, tools)
	r.rows[cacheKey(sourceID, ws)] = cp
	return nil
}

func (r *ToolCacheRepo) List(_ context.Context, sourceID, ws string) ([]mcp.CachedTool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.rows[cacheKey(sourceID, ws)]
	out := make([]mcp.CachedTool, len(src))
	copy(out, src)
	return out, nil
}

func (r *ToolCacheRepo) Purge(_ context.Context, sourceID, ws string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, cacheKey(sourceID, ws))
	return nil
}
