package framework

import "strings"

// DecideByPatterns returns allow/deny/ask based on deny-first then allow list.
func DecideByPatterns(target string, allowPatterns, denyPatterns []string, defaultDecision AgentPermissionLevel) (AgentPermissionLevel, string) {
	target = strings.TrimSpace(target)
	for _, pattern := range denyPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if MatchGlob(pattern, target) {
			return AgentPermissionDeny, pattern
		}
	}
	for _, pattern := range allowPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if MatchGlob(pattern, target) {
			return AgentPermissionAllow, pattern
		}
	}
	if defaultDecision == "" {
		defaultDecision = AgentPermissionAllow
	}
	return defaultDecision, ""
}

