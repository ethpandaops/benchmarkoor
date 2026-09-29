package blockdev

import (
	"fmt"
	"os"
	"strings"
)

// mountEntry is one line of /proc/<pid>/mountinfo.
type mountEntry struct {
	MajMin       string
	MountPoint   string
	FSType       string
	Source       string
	SuperOptions string
}

// superOption returns the value of key in the superblock options, e.g. the
// "upperdir" of an overlay mount.
func (m mountEntry) superOption(key string) string {
	for opt := range strings.SplitSeq(m.SuperOptions, ",") {
		if value, ok := strings.CutPrefix(opt, key+"="); ok {
			return value
		}
	}

	return ""
}

// readMountInfo reads and parses a mountinfo file.
func readMountInfo(path string) ([]mountEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return parseMountInfo(string(data)), nil
}

// parseMountInfo parses the mountinfo format:
//
//	36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//
// The optional fields end at the "-" separator. Malformed lines are skipped.
func parseMountInfo(data string) []mountEntry {
	lines := strings.Split(data, "\n")
	entries := make([]mountEntry, 0, len(lines))

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		sep := -1

		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i

				break
			}
		}

		if sep < 0 || sep+3 >= len(fields) {
			continue
		}

		entries = append(entries, mountEntry{
			MajMin:       fields[2],
			MountPoint:   unescapeMountField(fields[4]),
			FSType:       fields[sep+1],
			Source:       unescapeMountField(fields[sep+2]),
			SuperOptions: fields[sep+3],
		})
	}

	return entries
}

// findMount returns the mount of the device majMin. A device can be mounted
// more than once (bind mounts), and every entry has the same type and source.
func findMount(entries []mountEntry, majMin string) *mountEntry {
	for i := range entries {
		if entries[i].MajMin == majMin {
			return &entries[i]
		}
	}

	return nil
}

// findMountByPath returns the mount that holds path: the entry with the
// longest mount point that is a prefix of path. Of two entries on the same
// mount point, the later one hides the earlier one.
func findMountByPath(entries []mountEntry, path string) *mountEntry {
	var best *mountEntry

	for i := range entries {
		mp := entries[i].MountPoint

		under := path == mp || mp == "/" || strings.HasPrefix(path, mp+"/")
		if !under {
			continue
		}

		if best == nil || len(mp) >= len(best.MountPoint) {
			best = &entries[i]
		}
	}

	return best
}

// unescapeMountField decodes the octal escapes (\040 for a space) in a
// mountinfo field.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}

	var b strings.Builder

	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var v byte

			ok := true

			for _, c := range []byte(s[i+1 : i+4]) {
				if c < '0' || c > '7' {
					ok = false

					break
				}

				v = v*8 + (c - '0')
			}

			if ok {
				b.WriteByte(v)

				i += 3

				continue
			}
		}

		b.WriteByte(s[i])
	}

	return b.String()
}
