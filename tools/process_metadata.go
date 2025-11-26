package tools

import "time"

// ProcessMetadata captures runtime details for an external tool or server.
type ProcessMetadata struct {
	PID     int
	Command string
	Args    []string
	Started time.Time
}

// ProcessMetadataProvider exposes metadata for the hosting process.
type ProcessMetadataProvider interface {
	ProcessMetadata() ProcessMetadata
}

// LogEmitter provides stderr/stdout lines for inspection.
type LogEmitter interface {
	Logs() <-chan string
}

// ProxyInstance bundles runtime metadata for a language-specific proxy.
type ProxyInstance struct {
	Language string
	Command  string
	PID      int
	Started  time.Time
	Logs     <-chan string
}
