package tools_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/tools"
)

type Service struct {
	handlers map[string]tools.Handler
}

func NewService(handlers ...tools.Handler) *Service {
	service := &Service{handlers: make(map[string]tools.Handler)}
	for _, handler := range handlers {
		service.Register(handler)
	}
	return service
}

func (s *Service) Register(handler tools.Handler) {
	if s == nil || handler == nil {
		return
	}
	if s.handlers == nil {
		s.handlers = make(map[string]tools.Handler)
	}
	name := strings.ToLower(strings.TrimSpace(handler.Definition().Name))
	if name == "" {
		return
	}
	s.handlers[name] = handler
}

func (s *Service) Definitions() []tools.Definition {
	defs := make([]tools.Definition, 0, len(s.handlers))
	for _, handler := range s.handlers {
		defs = append(defs, handler.Definition())
	}
	return defs
}

func (s *Service) DefinitionsFor(visibility tools.ToolVisibility) []tools.Definition {
	defs := make([]tools.Definition, 0, len(s.handlers))
	for _, handler := range s.handlers {
		def := handler.Definition()
		if def.IsVisibleIn(visibility) {
			defs = append(defs, def)
		}
	}
	return defs
}

func (s *Service) Execute(ctx context.Context, name string, params map[string]interface{}) (tools.ExecutionResult, error) {
	if s == nil {
		return tools.ExecutionResult{}, fmt.Errorf("tool service unavailable")
	}
	handler, ok := s.handlers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return tools.ExecutionResult{}, fmt.Errorf("tool %s not found", name)
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	log.Printf("[Tool Service] Executing tool: %s with params: %v", name, params)
	result, err := handler.Execute(ctx, params)
	if err != nil {
		log.Printf("[Tool Service] Tool %s FAILED: %v", name, err)
	} else {
		log.Printf("[Tool Service] Tool %s SUCCESS: %+v", name, result.Result)
	}
	return result, err
}

func (s *Service) ExecuteWithConfig(ctx context.Context, name string, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	if s == nil {
		return tools.ExecutionResult{}, fmt.Errorf("tool service unavailable")
	}
	handler, ok := s.handlers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return tools.ExecutionResult{}, fmt.Errorf("tool %s not found", name)
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	if config == nil {
		config = map[string]interface{}{}
	}
	log.Printf("[Tool Service] Executing tool with config: %s, config: %v, params: %v", name, redactToolConfig(config), params)
	result, err := handler.ExecuteWithConfig(ctx, config, params)
	if err != nil {
		log.Printf("[Tool Service] Tool %s FAILED: %v", name, err)
	} else {
		log.Printf("[Tool Service] Tool %s SUCCESS: %+v", name, result.Result)
	}
	return result, err
}

func (s *Service) Handler(name string) (tools.Handler, bool) {
	if s == nil {
		return nil, false
	}
	handler, ok := s.handlers[strings.ToLower(strings.TrimSpace(name))]
	return handler, ok
}

func redactToolConfig(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return nil
	}
	redacted := make(map[string]interface{}, len(config))
	for key, value := range config {
		if isSensitiveToolConfigKey(key) {
			redacted[key] = "[REDACTED]"
			continue
		}
		redacted[key] = value
	}
	return redacted
}

func isSensitiveToolConfigKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	sensitiveParts := []string{"password", "secret", "token", "api_key", "apikey", "authorization", "credential"}
	for _, part := range sensitiveParts {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

var _ tools.Service = (*Service)(nil)
