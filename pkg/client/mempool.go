package client

import (
	"fmt"
	"slices"
	"strings"
)

// PoolLimits are the txpool limits a mempool profile pins, so that a
// comparison between clients compares implementations, not defaults.
type PoolLimits struct {
	AccountSlots  int
	GlobalSlots   int
	AccountQueue  int
	GlobalQueue   int
	PriceBump     int
	BlobPriceBump int
}

// MempoolSettings are the inputs of a mempool profile.
type MempoolSettings struct {
	Limits PoolLimits
	// GasLimit is the block gas limit the client targets when it builds.
	GasLimit uint64
	// P2PPort is the devp2p listen port. Discovery stays off, so a peer
	// can only dial in.
	P2PPort int
}

// MempoolProfile is an optional Spec capability: the client can run in the
// mempool executor mode. A client without it is rejected in that mode.
type MempoolProfile interface {
	// MempoolCommand returns the command for the mempool mode. Compared to
	// DefaultCommand it listens for devp2p peers (discovery off), exposes the
	// txpool RPC namespace, keeps the pool on and pins the pool limits.
	MempoolCommand(settings MempoolSettings) []string
}

// DefaultMempoolP2PPort is the devp2p listen port of the mempool profile.
const DefaultMempoolP2PPort = 30303

// MempoolCommandFor returns the mempool command of spec, or an error when the
// client has no mempool profile yet.
func MempoolCommandFor(spec Spec, settings MempoolSettings) ([]string, error) {
	profile, ok := spec.(MempoolProfile)
	if !ok {
		return nil, fmt.Errorf("client %q has no mempool profile yet", spec.Type())
	}

	return profile.MempoolCommand(settings), nil
}

// SupportsMempool returns true when spec has a mempool profile.
func SupportsMempool(spec Spec) bool {
	_, ok := spec.(MempoolProfile)

	return ok
}

// replaceFlags returns cmd with every flag in replace swapped for its new
// value. A key is a flag prefix such as "--maxpeers=". A key with no match in
// cmd is appended.
func replaceFlags(cmd []string, replace map[string]string) []string {
	out := make([]string, 0, len(cmd)+len(replace))
	used := make(map[string]bool, len(replace))

	for _, arg := range cmd {
		replaced := false

		for prefix, value := range replace {
			if strings.HasPrefix(arg, prefix) {
				out = append(out, value)
				used[prefix] = true
				replaced = true

				break
			}
		}

		if !replaced {
			out = append(out, arg)
		}
	}

	// Append the flags that were not in cmd, in a stable order.
	missing := make([]string, 0, len(replace))

	for prefix := range replace {
		if !used[prefix] {
			missing = append(missing, prefix)
		}
	}

	slices.Sort(missing)

	for _, prefix := range missing {
		out = append(out, replace[prefix])
	}

	return out
}
