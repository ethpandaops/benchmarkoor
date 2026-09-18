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
		selected = make([]int, 0, count)
		cores := physicalCores(cpus)

		// Take random cores while they fit, largest first. A hybrid host can
		// have cores of different sizes, and the small ones fill the rest.
		sort.SliceStable(cores, func(i, j int) bool { return len(cores[i]) > len(cores[j]) })

		for _, threads := range cores {
			if len(selected) == count {
				break
			}

			if len(selected)+len(threads) <= count {
				selected = append(selected, threads...)
			}
		}

		if len(selected) != count {
			return nil, fmt.Errorf(
				"cpuset_count %d cannot be filled with full cores on this host (%s)",
				count, Summary(cpus, nil))
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
