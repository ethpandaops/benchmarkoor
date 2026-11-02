// Package scenario holds the mempool scenarios and the code they share. A
// scenario is Go code (scenarios need logic, so there is no declarative DSL),
// registered by name and parameterised from YAML.
package scenario

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// Scenario is one mempool test.
type Scenario interface {
	// Name is the registry key and the test name in the results.
	Name() string
	// Description says what the scenario checks and measures.
	Description() string
	// Defaults are the parameters and their default values. A parameter
	// that is not here is rejected.
	Defaults() Params
	// Accounts returns how many pre-funded accounts the scenario needs.
	Accounts(p Params) int
	// Run runs the scenario. It returns an error only when it cannot go on
	// (RPC failure, timeout). Failed checks go into env.Rec.
	Run(ctx context.Context, env *Env) error
}

// PoolLimits are the pinned pool limits. Scenarios size their load from them.
type PoolLimits struct {
	AccountSlots  int `json:"account_slots"`
	GlobalSlots   int `json:"global_slots"`
	AccountQueue  int `json:"account_queue"`
	GlobalQueue   int `json:"global_queue"`
	PriceBump     int `json:"price_bump"`
	BlobPriceBump int `json:"blob_price_bump"`
}

// BurnerAddress holds a contract that loops until it runs out of gas, so a
// call to it uses its whole gas limit. The genesis predeploys it.
var BurnerAddress = common.HexToAddress("0x0000000000000000000000000000000000b0b0b0")

// BurnerCode is JUMPDEST PUSH0 JUMP: an endless loop.
var BurnerCode = []byte{0x5b, 0x5f, 0x56}

// SweeperAddress holds code that sends the whole balance of the executing
// account to the caller. An EOA that delegates to it (EIP-7702) can be
// emptied by anyone, which is how the scenarios drain a balance without a
// tx from its owner. The genesis predeploys it.
var SweeperAddress = common.HexToAddress("0x00000000000000000000000000000000005e3e9e")

// SweeperCode is CALL(gas, CALLER, SELFBALANCE, 0, 0, 0, 0); STOP.
var SweeperCode = []byte{0x5f, 0x5f, 0x5f, 0x5f, 0x47, 0x33, 0x5a, 0xf1, 0x00}

// ReverterAddress holds code that always reverts. A call to it is a valid tx
// that is included, uses its nonce and fails (receipt status 0).
var ReverterAddress = common.HexToAddress("0x0000000000000000000000000000000000ae7e27")

// ReverterCode is REVERT(0, 0).
var ReverterCode = []byte{0x5f, 0x5f, 0xfd}

// Chain describes the generated chain.
type Chain struct {
	ChainID  uint64
	GasLimit uint64
	// MaxTxGas is the EIP-7825 per-tx gas cap.
	MaxTxGas uint64
	// MaxBlobsPerBlock is the blob limit of the active fork.
	MaxBlobsPerBlock int
}

// Env is what a scenario runs against.
type Env struct {
	Log     logrus.FieldLogger
	Driver  cldriver.Driver
	Pool    observer.Observer
	Factory txfactory.Factory
	Rec     *Recorder
	Params  Params
	Limits  PoolLimits
	Chain   Chain

	inv invariantState

	// accountBase and accountCount are the account range of the scenario.
	accountBase  int
	accountCount int
	accountNext  int
}

// NewEnv returns an Env. The scenario may use the accounts
// [accountBase, accountBase+accountCount).
func NewEnv(base Env, accountBase, accountCount int) *Env {
	env := base
	env.accountBase = accountBase
	env.accountCount = accountCount
	env.accountNext = accountBase

	return &env
}

// Accounts hands out the next n accounts of the scenario range, with their
// nonces read from the chain.
func (e *Env) Accounts(ctx context.Context, n int) ([]*txfactory.Account, error) {
	if e.accountNext+n > e.accountBase+e.accountCount {
		return nil, fmt.Errorf("scenario needs more than its %d accounts", e.accountCount)
	}

	accounts, err := e.Factory.Accounts(e.accountNext, n)
	if err != nil {
		return nil, err
	}

	e.accountNext += n

	if err := e.syncNonces(ctx, accounts); err != nil {
		return nil, err
	}

	return accounts, nil
}

// Registry holds the scenarios by name, in registration order.
type Registry struct {
	order  []string
	byName map[string]Scenario
}

// NewRegistry returns a registry with every built-in scenario.
func NewRegistry() *Registry {
	r := &Registry{byName: make(map[string]Scenario, 8)}

	r.Register(&admission{})
	r.Register(&rejections{})
	r.Register(&nonceGap{})
	r.Register(&replacement{})
	r.Register(&ordering{})
	r.Register(&importFullPool{})
	r.Register(&reorg{})
	r.Register(&eviction{})
	r.Register(&baseFee{})
	r.Register(&payloadLatency{})
	r.Register(&sustainedLoad{})
	r.Register(&setcode{})
	r.Register(&blobPool{})
	r.Register(&balanceChanges{})

	return r
}

// Register adds a scenario.
func (r *Registry) Register(s Scenario) {
	if _, ok := r.byName[s.Name()]; !ok {
		r.order = append(r.order, s.Name())
	}

	r.byName[s.Name()] = s
}

// Get returns a scenario by name.
func (r *Registry) Get(name string) (Scenario, error) {
	s, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("unknown mempool scenario %q (known: %v)", name, r.order)
	}

	return s, nil
}

// Names returns every scenario name in registration order.
func (r *Registry) Names() []string {
	return slices.Clone(r.order)
}

// Params are scenario parameters from YAML. Numbers from YAML can be int or
// float64, so the getters convert.
type Params map[string]any

// Merge returns the defaults overridden by p. It rejects unknown keys.
func (p Params) Merge(defaults Params) (Params, error) {
	out := maps.Clone(defaults)
	if out == nil {
		out = make(Params, len(p))
	}

	keys := slices.Collect(maps.Keys(p))
	sort.Strings(keys)

	for _, k := range keys {
		if _, ok := defaults[k]; !ok {
			return nil, fmt.Errorf("unknown parameter %q", k)
		}

		out[k] = p[k]
	}

	return out, nil
}

// Int returns an integer parameter.
func (p Params) Int(key string) int {
	switch v := p[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(math.Round(v))
	default:
		return 0
	}
}

// Duration returns a duration parameter, written as "250ms" or as a number
// of milliseconds.
func (p Params) Duration(key string) time.Duration {
	switch v := p[key].(type) {
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0
		}

		return d
	default:
		return time.Duration(p.Int(key)) * time.Millisecond
	}
}

// Strings returns a list-of-strings parameter.
func (p Params) Strings(key string) []string {
	switch v := p[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, s := range v {
			out = append(out, fmt.Sprint(s))
		}

		return out
	default:
		return nil
	}
}
