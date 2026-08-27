package tool

import (
	"context"
	"fmt"
	"strings"
)

type Registry struct {
	tools    map[string]Tool
	allow    map[string]bool
	basePath string
}

func NewRegistry(basePath string) *Registry {
	return &Registry{
		tools:    make(map[string]Tool),
		allow:    make(map[string]bool),
		basePath: basePath,
	}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) SetAllowed(name string, allowed bool) {
	r.allow[name] = allowed
}

func (r *Registry) IsAllowed(name string) bool {
	if v, ok := r.allow[name]; ok {
		return v
	}
	return false
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) AllSchemas() []map[string]any {
	var schemas []map[string]any
	for _, t := range r.tools {
		schemas = append(schemas, t.Schema())
	}
	return schemas
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	if !r.IsAllowed(name) {
		return "", fmt.Errorf("工具 %s 未被授权（请在角色配置中添加）", name)
	}
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("未知工具: %s", name)
	}
	res := Execute(ctx, t, args)
	if res.Error != "" {
		return "", fmt.Errorf(res.Error)
	}
	return res.Output, nil
}

func (r *Registry) ListAllowed() []string {
	var list []string
	for name, ok := range r.allow {
		if ok {
			list = append(list, name)
		}
	}
	return list
}

func (r *Registry) AllToolNames() []string {
	var list []string
	for name := range r.tools {
		list = append(list, name)
	}
	return list
}

func NewDefaultRegistry(basePath string) *Registry {
	r := NewRegistry(basePath)
	r.Register(NewReadTool(basePath))
	r.Register(NewWriteTool(basePath))
	r.Register(NewEditTool(basePath))
	r.Register(NewBashTool(basePath))
	return r
}

func FormatSchemasForPrompt(schemas []map[string]any) string {
	var sb strings.Builder
	for _, s := range schemas {
		name := s["name"]
		if name == nil {
			if t, ok := s["title"].(string); ok {
				name = t
			}
		}
		if name == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, s["description"]))
	}
	return sb.String()
}
