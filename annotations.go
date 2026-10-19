package actions

// Annotations are hints about an action for tool hosts, such as MCP clients,
// written on its OpenAPI operation as the x-mcp-annotations extension with all
// four hints: readOnlyHint, destructiveHint, idempotentHint, and openWorldHint.
// They describe the action to a host deciding how to call it; nothing enforces
// them. Freeze refuses an action that is both ReadOnly and Destructive.
type Annotations struct {
	ReadOnly    bool // reads only; changes nothing
	Destructive bool // may delete or overwrite
	Idempotent  bool // repeating it with the same input has no further effect
	OpenWorld   bool // reaches systems outside this service
}

// clone returns a copy of a, or nil when a is nil, so a registered action
// keeps the hints it was registered with.
func (a *Annotations) clone() *Annotations {
	if a == nil {
		return nil
	}
	c := *a
	return &c
}

// extension returns the annotations as the x-mcp-annotations value.
func (a *Annotations) extension() map[string]any {
	return map[string]any{
		"readOnlyHint":    a.ReadOnly,
		"destructiveHint": a.Destructive,
		"idempotentHint":  a.Idempotent,
		"openWorldHint":   a.OpenWorld,
	}
}
