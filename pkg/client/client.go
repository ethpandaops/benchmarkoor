package client

import (
	"fmt"
	"sync"
)

// ClientType represents supported EL clients.
type ClientType string

const (
	ClientGeth       ClientType = "geth"
	ClientNethermind ClientType = "nethermind"
	ClientBesu       ClientType = "besu"
	ClientErigon     ClientType = "erigon"
	ClientNimbusEL   ClientType = "nimbus-el"
	ClientReth       ClientType = "reth"
)

// Spec provides client-specific container configuration.
type Spec interface {
	// Type returns the client type.
	Type() ClientType

	// DefaultImage returns the default Docker image.
	DefaultImage() string

	// DefaultCommand returns the default command arguments.
	DefaultCommand() []string

	// RequiresInit returns true if client needs init container.
	RequiresInit() bool

	// InitCommand returns the init container command (if RequiresInit is true).
	InitCommand() []string

	// DataDir returns the data directory path inside container.
	DataDir() string

	// GenesisPath returns the genesis file path inside container.
	GenesisPath() string

	// JWTPath returns the JWT secret file path inside container.
	JWTPath() string

	// RPCPort returns the JSON-RPC port.
	RPCPort() int

	// EnginePort returns the Engine API port.
	EnginePort() int

	// MetricsPort returns the metrics port.
	MetricsPort() int

	// DefaultEnvironment returns default environment variables for the client.
	DefaultEnvironment() map[string]string
}

// Registry manages client specifications.
type Registry interface {
	Get(clientType ClientType) (Spec, error)
	Register(spec Spec)
	List() []ClientType
}

// NewRegistry creates a registry with all supported clients.
func NewRegistry() Registry {
	r := &registry{
		specs: make(map[ClientType]Spec, 6),
	}

	// Register all supported clients.
	r.Register(NewGethSpec())
	r.Register(NewNethermindSpec())
	r.Register(NewBesuSpec())
	r.Register(NewErigonSpec())
	r.Register(NewNimbusSpec())
	r.Register(NewRethSpec())

	return r
}

type registry struct {
	mu    sync.RWMutex
	specs map[ClientType]Spec
}

// Ensure interface compliance.
var _ Registry = (*registry)(nil)

// Get returns the spec for the given client type.
func (r *registry) Get(clientType ClientType) (Spec, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, ok := r.specs[clientType]
	if !ok {
		return nil, fmt.Errorf("unknown client type: %s", clientType)
	}

	return spec, nil
}

// Register adds a spec to the registry.
func (r *registry) Register(spec Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.specs[spec.Type()] = spec
}

// List returns all registered client types.
func (r *registry) List() []ClientType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]ClientType, 0, len(r.specs))
	for t := range r.specs {
		types = append(types, t)
	}

	return types
}
