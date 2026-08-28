package executor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// stepReadBufferSize is the read buffer for step files. A single JSON-RPC
// line carries a whole block payload, so it is routinely megabytes; a small
// buffer just means more syscalls per line.
const stepReadBufferSize = 1 << 20

// lineSource yields a step's JSON-RPC lines in order.
//
// A step file is usually small, but a stateful pre-run bundle is not: one
// reached 46 GiB, and reading it into a []string before replaying it took the
// whole runner with it (RSS 56 GiB, OOM-killed). The file-backed source
// therefore holds one line at a time and nothing else.
type lineSource interface {
	// Total is how many lines the source yields, for progress and ETA.
	Total() int
	// Next returns the next line. ok is false once the source is exhausted.
	Next() (line string, ok bool, err error)
	// Close releases anything the source holds.
	Close() error
}

// sliceLineSource serves lines already in memory, for provider-backed steps.
type sliceLineSource struct {
	lines []string
	pos   int
}

func newSliceLineSource(lines []string) *sliceLineSource {
	return &sliceLineSource{lines: lines}
}

func (s *sliceLineSource) Total() int { return len(s.lines) }

func (s *sliceLineSource) Next() (string, bool, error) {
	if s.pos >= len(s.lines) {
		return "", false, nil
	}

	line := s.lines[s.pos]
	s.pos++

	return line, true, nil
}

func (s *sliceLineSource) Close() error { return nil }

// fileLineSource streams lines off disk, never holding more than the current
// one. Counting up front costs a second sequential pass, which keeps the
// progress and ETA output identical to what buffering the file produced.
type fileLineSource struct {
	file   *os.File
	reader *bufio.Reader
	total  int
	done   bool
}

func newFileLineSource(path string) (*fileLineSource, error) {
	total, err := countStepLines(path)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening step file: %w", err)
	}

	return &fileLineSource{
		file:   file,
		reader: bufio.NewReaderSize(file, stepReadBufferSize),
		total:  total,
	}, nil
}

func (s *fileLineSource) Total() int { return s.total }

func (s *fileLineSource) Next() (string, bool, error) {
	for {
		if s.done {
			return "", false, nil
		}

		line, err := s.reader.ReadString('\n')

		// A final line without a trailing newline arrives together with EOF,
		// so check the content before acting on the error.
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			if err != nil {
				s.done = true
				if err != io.EOF {
					return "", false, fmt.Errorf("reading step file: %w", err)
				}
			}

			return trimmed, true, nil
		}

		if err != nil {
			s.done = true

			if err == io.EOF {
				return "", false, nil
			}

			return "", false, fmt.Errorf("reading step file: %w", err)
		}
	}
}

func (s *fileLineSource) Close() error { return s.file.Close() }

// countStepLines counts the non-empty lines of a step file without keeping
// any of them.
func countStepLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("opening step file: %w", err)
	}

	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, stepReadBufferSize)
	count := 0

	for {
		line, err := reader.ReadString('\n')
		if strings.TrimSpace(line) != "" {
			count++
		}

		if err != nil {
			if err == io.EOF {
				return count, nil
			}

			return 0, fmt.Errorf("reading step file: %w", err)
		}
	}
}
