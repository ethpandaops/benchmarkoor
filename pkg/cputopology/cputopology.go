// Package cputopology reads the CPU topology of the host from sysfs. It maps
// each logical CPU (thread) to its physical core, socket, and NUMA node so a
// cpuset can be described in terms of physical cores rather than thread IDs.
package cputopology

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultSysfsPath is the sysfs directory that holds the cpu and node trees.
const DefaultSysfsPath = "/sys/devices/system"

// CPU describes one logical CPU (hardware thread).
type CPU struct {
	// ID is the logical CPU number as used by cpuset.
	ID int `json:"id"`
	// Core is the physical core ID. It is unique within a socket only.
	Core int `json:"core"`
	// Socket is the physical package ID.
	Socket int `json:"socket"`
	// NUMA is the NUMA node the CPU belongs to.
	NUMA int `json:"numa"`
}

// Read reads the topology of all online CPUs from the sysfs tree rooted at
// basePath. Pass DefaultSysfsPath outside of tests. It returns an error when
// the tree is missing, for example on a non-Linux host.
func Read(basePath string) ([]CPU, error) {
	cpuDir := filepath.Join(basePath, "cpu")

	ids, err := readOnlineCPUs(cpuDir)
	if err != nil {
		return nil, err
	}

	numaByCPU, err := readNUMANodes(filepath.Join(basePath, "node"))
	if err != nil {
		return nil, err
	}

	cpus := make([]CPU, 0, len(ids))

	for _, id := range ids {
		topoDir := filepath.Join(cpuDir, fmt.Sprintf("cpu%d", id), "topology")

		core, err := readInt(filepath.Join(topoDir, "core_id"))
		if err != nil {
			return nil, fmt.Errorf("reading core_id of cpu%d: %w", id, err)
		}

		socket, err := readInt(filepath.Join(topoDir, "physical_package_id"))
		if err != nil {
			return nil, fmt.Errorf("reading physical_package_id of cpu%d: %w", id, err)
		}

		cpus = append(cpus, CPU{
			ID:     id,
			Core:   core,
			Socket: socket,
			NUMA:   numaByCPU[id],
		})
	}

	return cpus, nil
}

// readOnlineCPUs returns the sorted list of online CPU IDs.
func readOnlineCPUs(cpuDir string) ([]int, error) {
	for _, name := range []string{"online", "present"} {
		data, err := os.ReadFile(filepath.Join(cpuDir, name))
		if err != nil {
			continue
		}

		ids, err := ParseCPUList(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parsing %s CPUs: %w", name, err)
		}

		return ids, nil
	}

	return nil, fmt.Errorf("reading %s: no online or present file", cpuDir)
}

// readNUMANodes maps each CPU ID to its NUMA node. A host without a node tree
// maps every CPU to node 0.
func readNUMANodes(nodeDir string) (map[int]int, error) {
	lists, err := filepath.Glob(filepath.Join(nodeDir, "node[0-9]*", "cpulist"))
	if err != nil {
		return nil, fmt.Errorf("listing NUMA nodes: %w", err)
	}

	numaByCPU := make(map[int]int, 64)

	for _, list := range lists {
		nodeName := filepath.Base(filepath.Dir(list))

		node, err := strconv.Atoi(strings.TrimPrefix(nodeName, "node"))
		if err != nil {
			return nil, fmt.Errorf("parsing NUMA node name %q: %w", nodeName, err)
		}

		data, err := os.ReadFile(list)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", list, err)
		}

		ids, err := ParseCPUList(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parsing cpulist of %s: %w", nodeName, err)
		}

		for _, id := range ids {
			numaByCPU[id] = node
		}
	}

	return numaByCPU, nil
}

// readInt reads a single integer from a sysfs file.
func readInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", path, err)
	}

	return value, nil
}

// ParseCPUList parses a kernel CPU list such as "0-3,8,10-11" into sorted,
// distinct CPU IDs. An empty string yields an empty list.
func ParseCPUList(list string) ([]int, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return []int{}, nil
	}

	parts := strings.Split(list, ",")
	seen := make(map[int]struct{}, len(parts))
	ids := make([]int, 0, len(parts))

	add := func(id int) {
		if _, ok := seen[id]; ok {
			return
		}

		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty entry in CPU list %q", list)
		}

		start, end, found := strings.Cut(part, "-")
		if !found {
			id, err := strconv.Atoi(start)
			if err != nil {
				return nil, fmt.Errorf("parsing CPU %q: %w", part, err)
			}

			add(id)

			continue
		}

		from, err := strconv.Atoi(start)
		if err != nil {
			return nil, fmt.Errorf("parsing range start %q: %w", part, err)
		}

		to, err := strconv.Atoi(end)
		if err != nil {
			return nil, fmt.Errorf("parsing range end %q: %w", part, err)
		}

		if to < from {
			return nil, fmt.Errorf("invalid range %q", part)
		}

		for id := from; id <= to; id++ {
			add(id)
		}
	}

	sort.Ints(ids)

	return ids, nil
}

// coreKey identifies a physical core across sockets.
type coreKey struct {
	socket int
	core   int
}

// Summary describes a cpuset in terms of the host topology, for example
// "6 threads on 3 of 6 physical cores (2 of 2 threads per core)". When cpuset
// is empty it describes the host instead: "12 threads on 6 physical cores
// (2 threads per core)". NUMA and socket counts are appended only when the
// host has more than one.
func Summary(cpus []CPU, cpuset []int) string {
	if len(cpus) == 0 {
		return ""
	}

	byID := make(map[int]CPU, len(cpus))
	threadsPerCore := make(map[coreKey]int, len(cpus))
	sockets := make(map[int]struct{}, 2)
	nodes := make(map[int]struct{}, 2)

	for _, c := range cpus {
		byID[c.ID] = c
		threadsPerCore[coreKey{c.Socket, c.Core}]++
		sockets[c.Socket] = struct{}{}
		nodes[c.NUMA] = struct{}{}
	}

	hostThreadsPerCore := 0
	for _, n := range threadsPerCore {
		hostThreadsPerCore = max(hostThreadsPerCore, n)
	}

	if len(cpuset) == 0 {
		s := fmt.Sprintf("%d threads on %d physical cores (%s per core)",
			len(cpus), len(threadsPerCore), plural(hostThreadsPerCore, "thread"))

		if len(sockets) > 1 {
			s += fmt.Sprintf(", %d sockets", len(sockets))
		}

		if len(nodes) > 1 {
			s += fmt.Sprintf(", %d NUMA nodes", len(nodes))
		}

		return s
	}

	usedPerCore := make(map[coreKey]int, len(cpuset))
	usedSockets := make(map[int]struct{}, 2)
	usedNodes := make(map[int]struct{}, 2)
	usedThreads := 0

	for _, id := range cpuset {
		c, ok := byID[id]
		if !ok {
			continue
		}

		usedThreads++
		usedPerCore[coreKey{c.Socket, c.Core}]++
		usedSockets[c.Socket] = struct{}{}
		usedNodes[c.NUMA] = struct{}{}
	}

	if usedThreads == 0 {
		return fmt.Sprintf("cpuset matches none of the %d host threads", len(cpus))
	}

	minUsed, maxUsed := hostThreadsPerCore, 0
	for _, n := range usedPerCore {
		minUsed = min(minUsed, n)
		maxUsed = max(maxUsed, n)
	}

	perCore := strconv.Itoa(minUsed)
	if minUsed != maxUsed {
		perCore = fmt.Sprintf("%d-%d", minUsed, maxUsed)
	}

	s := fmt.Sprintf("%s on %d of %d physical cores (%s of %d threads per core)",
		plural(usedThreads, "thread"), len(usedPerCore), len(threadsPerCore),
		perCore, hostThreadsPerCore)

	if len(sockets) > 1 {
		s += fmt.Sprintf(", %d of %d sockets", len(usedSockets), len(sockets))
	}

	if len(nodes) > 1 {
		s += fmt.Sprintf(", %d of %d NUMA nodes", len(usedNodes), len(nodes))
	}

	return s
}

// plural formats a count with its unit, adding "s" when count is not 1.
func plural(count int, unit string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, unit)
	}

	return fmt.Sprintf("%d %ss", count, unit)
}
