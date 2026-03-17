package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/blocklog"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// prefixedWriter adds a prefix to each line written.
// If prefixFn is set, it is called per line to generate the prefix dynamically.
// Otherwise, the static prefix field is used.
type prefixedWriter struct {
	prefix   string
	prefixFn func() string
	writer   io.Writer
	buf      []byte
}

func (w *prefixedWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	w.buf = append(w.buf, p...)

	for {
		idx := -1

		for i, b := range w.buf {
			if b == '\n' {
				idx = i

				break
			}
		}

		if idx == -1 {
			break
		}

		line := w.buf[:idx+1]
		w.buf = w.buf[idx+1:]

		pfx := w.prefix
		if w.prefixFn != nil {
			pfx = w.prefixFn()
		}

		if _, err := fmt.Fprintf(w.writer, "%s%s", pfx, line); err != nil {
			return n, err
		}
	}

	return n, nil
}

// clientLogPrefix returns a function that generates a consistent log prefix
// for client container logs: "🟣 $TIMESTAMP CLIE | $clientName | ".
func clientLogPrefix(clientName string) func() string {
	return func() string {
		ts := time.Now().UTC().Format(config.LogTimestampFormat)

		return fmt.Sprintf("🟣 %s CLIE | %s | ", ts, clientName)
	}
}

// fileHook writes log entries to a file.
type fileHook struct {
	writer    io.Writer
	formatter logrus.Formatter
}

func (h *fileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *fileHook) Fire(entry *logrus.Entry) error {
	line, err := h.formatter.Format(entry)
	if err != nil {
		return err
	}

	_, err = h.writer.Write(line)

	return err
}

// streamLogs streams container logs to file and optionally stdout/benchmarkoor log.
// The log file should be opened in append mode before calling this function.
// If blockLogCollector is provided, the collector's writer wraps the file writer
// to intercept and parse JSON payloads from log lines.
func (r *runner) streamLogs(
	ctx context.Context,
	instanceID, containerID string,
	file *os.File,
	benchmarkoorLog io.Writer,
	logInfo *containerLogInfo,
	blockLogCollector blocklog.Collector,
) error {
	// Write start marker with container metadata.
	_, _ = fmt.Fprint(file, formatStartMarker("CONTAINER", logInfo))

	// Base writer is the file, optionally wrapped by block log collector.
	var baseWriter io.Writer = file
	if blockLogCollector != nil {
		baseWriter = blockLogCollector.Writer()
	}

	stdout, stderr := baseWriter, baseWriter

	if r.cfg.ClientLogsToStdout {
		pfxFn := clientLogPrefix(instanceID)
		stdoutPrefixWriter := &prefixedWriter{prefixFn: pfxFn, writer: os.Stdout}
		logFilePrefixWriter := &prefixedWriter{prefixFn: pfxFn, writer: benchmarkoorLog}
		stdout = io.MultiWriter(baseWriter, stdoutPrefixWriter, logFilePrefixWriter)
		stderr = io.MultiWriter(baseWriter, stdoutPrefixWriter, logFilePrefixWriter)
	}

	streamErr := r.containerMgr.StreamLogs(ctx, containerID, stdout, stderr)

	// Write end marker (best-effort, even if streaming failed).
	_, _ = fmt.Fprintf(file, "#CONTAINER:END\n")

	return streamErr
}

// startLogStreaming opens the container log file in append mode, registers a
// file-close cleanup, and launches a goroutine that streams container logs.
// It writes through logDone/logCancel so the caller (and waitForLogDrain)
// can drain and clean up the streaming goroutine.
func (r *runner) startLogStreaming(
	ctx context.Context,
	resultsDir string,
	instanceID, containerID string,
	benchmarkoorLog io.Writer,
	logInfo *containerLogInfo,
	blockLogCollector blocklog.Collector,
	cleanupStarted <-chan struct{},
	logDone *chan struct{},
	logCancel *context.CancelFunc,
	cleanupFuncs *[]func(),
) error {
	logCtx, cancel := context.WithCancel(ctx)
	*logCancel = cancel

	logFilePath := filepath.Join(resultsDir, "container.log")

	logFile, err := os.OpenFile(
		logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644,
	)
	if err != nil {
		cancel()

		return fmt.Errorf("opening container log file: %w", err)
	}

	*cleanupFuncs = append(*cleanupFuncs, func() { _ = logFile.Close() })

	done := make(chan struct{})
	*logDone = done

	r.wg.Add(1)

	go func() {
		defer r.wg.Done()
		defer close(done)

		if streamErr := r.streamLogs(
			logCtx, instanceID, containerID, logFile,
			benchmarkoorLog, logInfo, blockLogCollector,
		); streamErr != nil {
			select {
			case <-cleanupStarted:
			default:
				r.log.WithError(streamErr).Debug("Log streaming ended")
			}
		}
	}()

	return nil
}

// waitForLogDrain waits for the log-streaming goroutine to finish (signalled
// via logDone) up to the given timeout. If the timeout expires it cancels the
// log context and waits for the goroutine to acknowledge. This must be called
// *after* the container has been stopped so that Docker flushes buffered logs.
func waitForLogDrain(
	logDone *chan struct{},
	logCancel *context.CancelFunc,
	timeout time.Duration,
) {
	if logDone == nil || *logDone == nil {
		if logCancel != nil {
			(*logCancel)()
		}

		return
	}

	select {
	case <-*logDone:
	case <-time.After(timeout):
		(*logCancel)()
		<-*logDone
	}
}

// removeHook removes a hook from the logger.
func (r *runner) removeHook(hook logrus.Hook) {
	for level, hooks := range r.logger.Hooks {
		filtered := make([]logrus.Hook, 0, len(hooks))

		for _, h := range hooks {
			if h != hook {
				filtered = append(filtered, h)
			}
		}

		r.logger.Hooks[level] = filtered
	}
}
