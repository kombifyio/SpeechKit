package agentkit

import (
	"fmt"
	"sort"
	"sync"
)

// ToolRegistry is a thread-safe collection of tools indexed by Name.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty ToolRegistry.
func NewRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry. Returns ErrDuplicateTool if a tool
// with the same Name is already present, or ErrNilTool if the supplied
// Tool is nil or has an empty Name.
func (r *ToolRegistry) Register(t Tool) error {
	if t == nil {
		return ErrNilTool
	}
	name := t.Name()
	if name == "" {
		return ErrNilTool
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateTool, name)
	}
	r.tools[name] = t
	return nil
}

// MustRegister calls Register and panics on error. Convenient at init time.
func (r *ToolRegistry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Lookup returns the registered tool with the given name. The bool reports
// whether the tool exists.
func (r *ToolRegistry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns a snapshot of every registered tool, ordered by Name.
func (r *ToolRegistry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for k := range r.tools {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		out = append(out, r.tools[n])
	}
	return out
}

// Len returns the number of registered tools.
func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Definitions converts every registered tool into a ToolDefinition suitable
// for placing in LiveConfig.Tools.
func (r *ToolRegistry) Definitions() []ToolDefinition {
	tools := r.All()
	out := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolDefinition{
			Name:                 t.Name(),
			Description:          t.Description(),
			ParametersJSONSchema: t.InputSchema(),
		})
	}
	return out
}
