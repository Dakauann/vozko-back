package copilottools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"vozko/domain/agent/mcp"
	"vozko/domain/copilot"
	"vozko/domain/rag"
)

const knowledgeBaseLookupPageSize = 500

type agentCollections interface {
	ListByIDs(ctx context.Context, workspaceID string, ids []string) ([]*mcp.MCPCollection, error)
}

type AgentDeps struct {
	KnowledgeBases rag.ListKnowledgeBasesUseCase
	Collections    agentCollections
	Catalog        ToolCatalog
}

func (d AgentDeps) secrets() toolSecrets {
	return toolSecrets{catalog: d.Catalog}
}

func (d AgentDeps) knowledgeBaseNames(ctx context.Context, workspaceID string) map[string]string {
	names := map[string]string{}
	if d.KnowledgeBases == nil {
		return names
	}
	out, err := d.KnowledgeBases.Execute(ctx, workspaceID, nil, 1, knowledgeBaseLookupPageSize)
	if err != nil || out == nil {
		return names
	}
	for _, kb := range out.Items {
		if kb != nil {
			names[kb.ID] = kb.Name
		}
	}
	return names
}

func (d AgentDeps) collectionNames(ctx context.Context, workspaceID string, ids []string) map[string]string {
	names := map[string]string{}
	if d.Collections == nil || len(ids) == 0 {
		return names
	}
	collections, err := d.Collections.ListByIDs(ctx, workspaceID, ids)
	if err != nil {
		return names
	}
	for _, c := range collections {
		if c != nil && c.WorkspaceID == workspaceID {
			names[c.ID] = c.Name
		}
	}
	return names
}

func (d AgentDeps) requireLinks(ctx context.Context, workspaceID string, knowledgeBaseIDs, collectionIDs []string) error {
	if len(knowledgeBaseIDs) > 0 {
		known := d.knowledgeBaseNames(ctx, workspaceID)
		for _, id := range knowledgeBaseIDs {
			if _, ok := known[id]; !ok {
				return fmt.Errorf("%w: base de conhecimento desconhecida; use os ids de list_knowledge_bases", errInvalidArgs)
			}
		}
	}
	if len(collectionIDs) > 0 {
		known := d.collectionNames(ctx, workspaceID, collectionIDs)
		for _, id := range collectionIDs {
			if _, ok := known[id]; !ok {
				return fmt.Errorf("%w: coleção MCP desconhecida neste workspace", errInvalidArgs)
			}
		}
	}
	return nil
}

func (d AgentDeps) linkField(ctx context.Context, cc copilot.Context, key string, ids []string, names func(context.Context, string, []string) map[string]string) []copilot.Field {
	if len(ids) == 0 {
		return nil
	}
	known := names(ctx, cc.WorkspaceID, ids)
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := strings.TrimSpace(known[id]); name != "" {
			labels = append(labels, name)
			continue
		}
		labels = append(labels, "desconhecida")
	}
	return []copilot.Field{{Key: key, Value: strings.Join(labels, "\n")}}
}

func (d AgentDeps) knowledgeBaseField(ctx context.Context, cc copilot.Context, key string, ids []string) []copilot.Field {
	return d.linkField(ctx, cc, key, ids, func(ctx context.Context, ws string, _ []string) map[string]string {
		return d.knowledgeBaseNames(ctx, ws)
	})
}

func (d AgentDeps) collectionField(ctx context.Context, cc copilot.Context, key string, ids []string) []copilot.Field {
	return d.linkField(ctx, cc, key, ids, d.collectionNames)
}

func agentFieldCard(f agentFields) []copilot.Field {
	var out []copilot.Field
	text := func(key string, value *string) {
		if value != nil {
			out = append(out, copilot.Field{Key: key, Value: strings.TrimSpace(*value)})
		}
	}
	flag := func(key string, value *bool) {
		if value != nil {
			out = append(out, copilot.Field{Key: key, Value: strconv.FormatBool(*value)})
		}
	}
	text("name", f.Name)
	text("description", f.Description)
	text("messagingModel", f.MessagingModel)
	text("provider", f.Provider)
	flag("isActive", f.IsActive)
	text("initialMessage", f.InitialMessage)
	flag("useInitialMessage", f.UseInitialMessage)
	text("avatarUrl", f.AvatarURL)
	if f.BusinessPhoneID != nil {
		out = append(out, copilot.Field{Key: "phone", Value: "muda o número de WhatsApp em que o agente responde"})
	}
	text("messagingPrompt", f.MessagingPrompt)
	return out
}

func toolBindingField(key string, bindings []toolBindingArg) []copilot.Field {
	if len(bindings) == 0 {
		return nil
	}
	lines := make([]string, 0, len(bindings))
	for _, b := range bindings {
		lines = append(lines, toolBindingLine(b))
	}
	return []copilot.Field{{Key: key, Value: strings.Join(lines, "\n")}}
}

func toolBindingLine(b toolBindingArg) string {
	if len(b.Config) == 0 {
		return b.Name
	}
	keys := make([]string, 0, len(b.Config))
	for k := range b.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+configValue(b.Config[k]))
	}
	return b.Name + " (" + strings.Join(parts, "; ") + ")"
}

func configValue(raw interface{}) string {
	switch v := raw.(type) {
	case map[string]interface{}:
		names := make([]string, 0, len(v))
		for k := range v {
			names = append(names, k)
		}
		sort.Strings(names)
		return strings.Join(names, ", ")
	case []interface{}:
		return strconv.Itoa(len(v)) + " itens"
	case nil:
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func namesField(key string, names []string) []copilot.Field {
	if len(names) == 0 {
		return nil
	}
	return []copilot.Field{{Key: key, Value: strings.Join(names, "\n")}}
}
