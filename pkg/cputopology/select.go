package cputopology

import (
	"fmt"
	mrand "math/rand/v2"
	"sort"
	"strings"
)

// Mode controls how a cpuset relates to physical cores.
type Mode string

const (
	// ModeAny places no constraint on the cpuset. This is the default.
	ModeAny Mode = "any"
	// ModeFullCores requires every physical core in the cpuset to be used
	// with all of its threads.
	ModeFullCores Mode = "full_cores"
	// ModeOneThreadPerCore requires at most one thread from each physical
	// core in the cpuset.
	ModeOneThreadPerCore Mode = "one_thread_per_core"
)

// ParseMode parses a cpuset_topology value. An empty string is ModeAny.
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.TrimSpace(s)) {
	case "", ModeAny:
		return ModeAny, nil
	case ModeFullCores:
		return ModeFullCores, nil
	case ModeOneThreadPerCore:
		return ModeOneThreadPerCore, nil
	default:
		return "", fmt.Errorf("invalid cpuset_topology %q (valid: %s, %s, %s)",
			s, ModeAny, ModeFullCores, ModeOneThreadPerCore)
	}
}

// physicalCores groups the threads of cpus by physical core, in a random
// order so a caller can take the first N.
func physicalCores(cpus []CPU) [][]int {
	byCore := make(map[coreKey][]int, len(cpus))
	order := make([]coreKey, 0, len(cpus))

	for _, c := range cpus {
		key := coreKey{c.Socket, c.Core}
		if _, ok := byCore[key]; !ok {
			order = append(order, key)
		}

		byCore[key] = append(byCore[key], c.ID)
	}

	mrand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	cores := make([][]int, 0, len(order))
	for _, key := range order {
		threads := byCore[key]
		sort.Ints(threads)
		cores = append(cores, threads)
	}

	return cores
}

// fillWholeCores picks cores whose thread counts add up to exactly count. It
// walks cores in the given order and keeps a core whenever the rest can still
// complete the sum, so a shuffled input yields a random valid combination. A
// hybrid host can mix core sizes, and a greedy fill would miss combinations
// such as 2+2 when a 3-thread core is taken first. It returns nil when no
// combination exists.
func fillWholeCores(cores [][]int, count int) [][]int {
	n := len(cores)

	// canFill[i][s] reports whether cores[i:] can reach sum s exactly.
	canFill := make([][]bool, n+1)
	for i := range canFill {
		canFill[i] = make([]bool, count+1)
	}

	canFill[n][0] = true

	for i := n - 1; i >= 0; i-- {
		size := len(cores[i])
		for s := 0; s <= count; s++ {
			canFill[i][s] = canFill[i+1][s] || (s >= size && canFill[i+1][s-size])
		}
	}

	if !canFill[0][count] {
		return nil
	}

	picked := make([][]int, 0, n)
	remaining := count

	for i := 0; i < n && remaining > 0; i++ {
		size := len(cores[i])
		if remaining >= size && canFill[i+1][remaining-size] {
			picked = append(picked, cores[i])
			remaining -= size
		}
	}

	return picked
}

// Select picks count random threads from cpus with the given mode. For
// ModeFullCores it picks whole random cores until count threads are
// reached. For ModeOneThreadPerCore it picks count random cores and takes
// the lowest thread ID of each. ModeAny picks any count threads. The result
// is sorted.
func Select(cpus []CPU, count int, mode Mode) ([]int, error) {
	if count < 1 {
		return nil, fmt.Errorf("cpuset_count must be at least 1, got %d", count)
	}

	if count > len(cpus) {
		return nil, fmt.Errorf("requested %d CPUs but only %d available", count, len(cpus))
	}

	var selected []int

	switch mode {
	case ModeAny:
		selected = make([]int, 0, count)
		ids := make([]int, 0, len(cpus))

		for _, c := range cpus {
			ids = append(ids, c.ID)
		}

		mrand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		selected = append(selected, ids[:count]...)

	case ModeFullCores:
		picked := fillWholeCores(physicalCores(cpus), count)
		if picked == nil {
			return nil, fmt.Errorf(
				"cpuset_count %d cannot be filled with full cores on this host (%s)",
				count, Summary(cpus, nil))
		}

		selected = make([]int, 0, count)
		for _, threads := range picked {
			selected = append(selected, threads...)
		}

	case ModeOneThreadPerCore:
		cores := physicalCores(cpus)
		if count > len(cores) {
			return nil, fmt.Errorf(
				"cpuset_count %d exceeds the %d physical cores needed for one thread per core",
				count, len(cores))
		}

		selected = make([]int, 0, count)
		for _, threads := range cores[:count] {
			selected = append(selected, threads[0])
		}

	default:
		return nil, fmt.Errorf("unknown cpuset_topology mode %q", mode)
	}

	sort.Ints(selected)

	return selected, nil
}

// Check verifies that an explicit cpuset satisfies mode on the given host.
// ModeAny always passes. Unknown CPU IDs fail.
func Check(cpus []CPU, cpuset []int, mode Mode) error {
	if mode == ModeAny {
		return nil
	}

	byID := make(map[int]CPU, len(cpus))
	threadsPerCore := make(map[coreKey]int, len(cpus))

	for _, c := range cpus {
		byID[c.ID] = c
		threadsPerCore[coreKey{c.Socket, c.Core}]++
	}

	usedPerCore := make(map[coreKey]int, len(cpuset))
	firstThread := make(map[coreKey]int, len(cpuset))

	for _, id := range cpuset {
		c, ok := byID[id]
		if !ok {
			return fmt.Errorf("cpuset contains CPU %d, which is not in the host topology", id)
		}

		key := coreKey{c.Socket, c.Core}
		if _, seen := usedPerCore[key]; !seen {
			firstThread[key] = id
		}

		usedPerCore[key]++
	}

	for key, used := range usedPerCore {
		switch mode {
		case ModeFullCores:
			if used != threadsPerCore[key] {
				return fmt.Errorf(
					"cpuset_topology %s: core %d on socket %d uses %d of %d threads",
					mode, key.core, key.socket, used, threadsPerCore[key])
			}
		case ModeOneThreadPerCore:
			if used > 1 {
				return fmt.Errorf(
					"cpuset_topology %s: core %d on socket %d uses %d threads (first: CPU %d)",
					mode, key.core, key.socket, used, firstThread[key])
			}
		default:
			return fmt.Errorf("unknown cpuset_topology mode %q", mode)
		}
	}

	return nil
}
