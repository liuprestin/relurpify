package agents

import "github.com/lexcodex/relurpify/framework"

// Mode enumerates the supported execution profiles for the coding agent.
type Mode string

const (
	ModeCode       Mode = "code"
	ModeArchitect  Mode = "architect"
	ModeAsk        Mode = "ask"
	ModeDebug      Mode = "debug"
	ModeDocument   Mode = "docs"
	defaultMode        = ModeCode
)

// ToolScope defines the rough permission envelope for a mode.
type ToolScope struct {
	AllowRead    bool
	AllowWrite   bool
	AllowExecute bool
	AllowNetwork bool
}

// ModeProfile bundles temperature, tooling envelope, and documentation for a
// mode so the orchestrator can enforce consistent behavior.
type ModeProfile struct {
	Name         Mode
	Title        string
	Description  string
	Temperature  float64
	Capabilities []framework.Capability
	ToolScope    ToolScope
	Restrictions []string
}

// defaultModeProfiles returns the baked-in description for every agent mode so
// the CLI can operate even before user manifests override the settings.
func defaultModeProfiles() map[Mode]ModeProfile {
	return map[Mode]ModeProfile{
		ModeCode: {
			Name:        ModeCode,
			Title:       "Code Mode",
			Description: "General-purpose development with read/write/execute access.",
			Temperature: 0.3,
			Capabilities: []framework.Capability{
				framework.CapabilityPlan,
				framework.CapabilityCode,
				framework.CapabilityExplain,
				framework.CapabilityRefactor,
			},
			ToolScope: ToolScope{
				AllowRead:    true,
				AllowWrite:   true,
				AllowExecute: true,
				AllowNetwork: false,
			},
		},
		ModeArchitect: {
			Name:        ModeArchitect,
			Title:       "Architect Mode",
			Description: "High-level architecture planning with read-only tools.",
			Temperature: 0.1,
			Capabilities: []framework.Capability{
				framework.CapabilityPlan,
				framework.CapabilityExplain,
			},
			ToolScope: ToolScope{
				AllowRead:    true,
				AllowWrite:   false,
				AllowExecute: false,
				AllowNetwork: false,
			},
			Restrictions: []string{
				"No filesystem writes",
				"No shell command execution",
			},
		},
		ModeAsk: {
			Name:        ModeAsk,
			Title:       "Ask Mode",
			Description: "Information retrieval, explanation, and documentation lookup.",
			Temperature: 0.2,
			Capabilities: []framework.Capability{
				framework.CapabilityExplain,
			},
			ToolScope: ToolScope{
				AllowRead:    true,
				AllowWrite:   false,
				AllowExecute: false,
				AllowNetwork: false,
			},
		},
		ModeDebug: {
			Name:        ModeDebug,
			Title:       "Debug Mode",
			Description: "Focused on diagnostics, log analysis, and running tests.",
			Temperature: 0.1,
			Capabilities: []framework.Capability{
				framework.CapabilityExplain,
				framework.CapabilityExecute,
			},
			ToolScope: ToolScope{
				AllowRead:    true,
				AllowWrite:   true,
				AllowExecute: true,
				AllowNetwork: false,
			},
		},
		ModeDocument: {
			Name:        ModeDocument,
			Title:       "Documentation Mode",
			Description: "Produces README and API docs; writes limited to doc files.",
			Temperature: 0.4,
			Capabilities: []framework.Capability{
				framework.CapabilityExplain,
				framework.CapabilityPlan,
			},
			ToolScope: ToolScope{
				AllowRead:    true,
				AllowWrite:   true,
				AllowExecute: false,
				AllowNetwork: false,
			},
			Restrictions: []string{
				"Write operations restricted to documentation paths.",
			},
		},
	}
}
