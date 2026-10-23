package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrWorkflowNotPublished = fmt.Errorf("%w: it was never published", ErrWorkflowNotActive)
	ErrVersionNotFound      = errors.New("workflow: version not found")
	ErrVersionNumberTaken   = errors.New("workflow: the version number was taken by a concurrent publish")
	ErrInvalidVersion       = errors.New("workflow: version must be a positive number")
)

const VersionsKeptPerWorkflow = 50

type WorkflowVersion struct {
	ID          string       `json:"id"`
	WorkflowID  string       `json:"workflowId"`
	Version     int          `json:"version"`
	Type        WorkflowType `json:"type"`
	Graph       *Graph       `json:"graph,omitempty"`
	GraphHash   string       `json:"-"`
	PublishedAt time.Time    `json:"publishedAt"`
	PublishedBy string       `json:"publishedBy,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`

	Handles map[string][]HandleDefinition `json:"handles,omitempty"`
}

type PublishVersion struct {
	WorkflowID    string
	Type          WorkflowType
	Graph         Graph
	GraphHash     string
	PublishedBy   string
	DraftRevision int
	RewriteDraft  bool
	Draft         Graph
}

func PublishVersionOf(w *Workflow, publishedBy string, rewriteDraft bool) PublishVersion {
	return PublishVersion{
		WorkflowID:    w.ID,
		Type:          w.Type,
		Graph:         StripDraftOnly(w.Graph),
		GraphHash:     GraphHash(&w.Graph),
		PublishedBy:   publishedBy,
		DraftRevision: w.Version,
		RewriteDraft:  rewriteDraft,
		Draft:         w.Graph,
	}
}

type VersionReader interface {
	FindVersion(workflowID string, version int) (*WorkflowVersion, error)
}

type PublishedReader interface {
	FindByID(workflowID string) (*Workflow, error)
	VersionReader
}

func GraphHash(g *Graph) string {
	published := StripDraftOnly(*g)
	b, err := json.Marshal(&published)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (w *Workflow) RefreshPublication() {
	w.HasUnpublishedChanges = w.PublishedVersion == nil || w.PublishedGraphHash == "" || GraphHash(&w.Graph) != w.PublishedGraphHash
}

func (w *Workflow) Running(v *WorkflowVersion) *Workflow {
	cp := *w
	cp.Graph = *v.Graph
	if v.Type.Valid() {
		cp.Type = v.Type
	}
	cp.TriggerType = cp.Graph.PrimaryTriggerType()
	number := v.Version
	cp.GraphVersion = &number
	return &cp
}

func (g Graph) Clone() Graph {
	out := Graph{}
	if g.Nodes != nil {
		out.Nodes = make([]Node, len(g.Nodes))
		for i, n := range g.Nodes {
			n.Config = cloneConfigMap(n.Config)
			if n.Pinned != nil {
				n.Pinned = n.Pinned.clone()
			}
			n.TestInputs = cloneTestInputs(n.TestInputs)
			out.Nodes[i] = n
		}
	}
	if g.Edges != nil {
		out.Edges = append([]Edge(nil), g.Edges...)
	}
	return out
}

func cloneConfigMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = cloneConfigValue(v)
	}
	return out
}

func cloneConfigValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return cloneConfigMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = cloneConfigValue(item)
		}
		return out
	case []string:
		return append([]string(nil), t...)
	default:
		return v
	}
}

func LoadPublished(store PublishedReader, workflowID string) (*Workflow, error) {
	w, err := store.FindByID(workflowID)
	if err != nil || w == nil {
		return w, err
	}
	if w.PublishedVersion == nil {
		if w.Status == WorkflowStatusActive {
			return nil, fmt.Errorf("%w: workflow %s", ErrWorkflowNotPublished, w.ID)
		}
		return w, nil
	}
	return withVersion(store, w, *w.PublishedVersion)
}

func PublishedOf(store VersionReader, w *Workflow) (*Workflow, error) {
	if w == nil || w.PublishedVersion == nil {
		return nil, nil
	}
	return withVersion(store, w, *w.PublishedVersion)
}

type PublishedWorkflow struct {
	Draft     *Workflow
	Published *Workflow
	Err       error
}

type PublishedWorkflows interface {
	PublishedInWorkspace(workspaceID string) ([]PublishedWorkflow, error)
}

type JoinedVersion struct {
	Version *WorkflowVersion
}

func (j JoinedVersion) FindVersion(string, int) (*WorkflowVersion, error) {
	return j.Version, nil
}

func FindVersionOf(store VersionReader, workflowID string, version int) (*WorkflowVersion, error) {
	v, err := store.FindVersion(workflowID, version)
	if err != nil {
		return nil, fmt.Errorf("read workflow %s version %d: %w", workflowID, version, err)
	}
	if v == nil || v.Graph == nil {
		return nil, fmt.Errorf("%w: workflow %s version %d", ErrVersionNotFound, workflowID, version)
	}
	return v, nil
}

func LoadForRun(store PublishedReader, run *WorkflowRun) (*Workflow, error) {
	w, err := store.FindByID(run.WorkflowID)
	if err != nil || w == nil {
		return w, err
	}
	return ForRun(store, w, run)
}

func ForRun(store VersionReader, w *Workflow, run *WorkflowRun) (*Workflow, error) {
	version := run.WorkflowVersion
	if version == nil {
		version = w.PublishedVersion
	}
	if version == nil {
		return w, nil
	}
	return withVersion(store, w, *version)
}

func withVersion(store VersionReader, w *Workflow, version int) (*Workflow, error) {
	v, err := FindVersionOf(store, w.ID, version)
	if err != nil {
		return nil, err
	}
	return w.Running(v), nil
}

type publisherKey struct{}

func WithPublisher(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, publisherKey{}, userID)
}

func PublisherFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	userID, _ := ctx.Value(publisherKey{}).(string)
	return userID
}

func (w *Workflow) RequiredCampaignVarsOf(published *WorkflowVersion) []string {
	if published != nil && published.Graph != nil {
		return ExtractCampaignVars(*published.Graph)
	}
	return ExtractCampaignVars(w.Graph)
}
