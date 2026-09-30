package blockdev

import (
	"context"
	"errors"
	"fmt"
	"math"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/docker/go-units"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// Probe defaults. The deep workloads follow the fio commands of EIP-7870:
// a 4 GiB file, 4 KiB random I/O with 75% reads, and 1 MiB sequential I/O
// with 50% reads, at an I/O depth of 64, with direct I/O.
const (
	DefaultProbeFileSize = 4 << 30
	DefaultProbeDuration = 5 * time.Second
	DefaultProbeIODepth  = 64

	randBlockSize = 4 << 10
	seqBlockSize  = 1 << 20

	// randReadPercent and seqReadPercent are the share of reads in the mixed
	// workloads, as fio --rwmixread=75 and fio --rw=readwrite in EIP-7870.
	randReadPercent = 75
	seqReadPercent  = 50

	// maxLatencyUs is the top of the latency histogram. A slower I/O counts
	// as maxLatencyUs, so a percentile above it shows as maxLatencyUs.
	maxLatencyUs = 20_000

	// directIOAlign is the buffer alignment that O_DIRECT needs.
	directIOAlign = 4096

	// minProbeFileSize keeps the random workloads away from a tiny file that
	// the drive cache holds entirely.
	minProbeFileSize = 64 << 20

	// probeFreeMargin is the free space that the probe leaves on the disk.
	probeFreeMargin = 1 << 30
)

// ProbeConfig configures a probe.
type ProbeConfig struct {
	// Dir is where the probe writes its temporary file.
	Dir string
	// FileSize is the size of the test file in bytes.
	FileSize int64
	// Duration is the time of each workload.
	Duration time.Duration
	// IODepth is the number of I/O operations in flight in the EIP-7870
	// workloads.
	IODepth int
}

// ProbeResult is the measured capacity of a device. The probe runs outside
// any container, so the values show the host device without throttles.
type ProbeResult struct {
	// The EIP-7870 workloads at IODepth. The random values come from one
	// mixed run with RandReadPercent reads, and the sequential values from
	// one mixed run with SeqReadPercent reads.
	RandReadIOPS  float64 `json:"rand_read_iops"`
	RandWriteIOPS float64 `json:"rand_write_iops"`
	SeqReadBps    float64 `json:"seq_read_bps"`
	SeqWriteBps   float64 `json:"seq_write_bps"`
	// RandReadPercent and SeqReadPercent are the share of reads in the mixed
	// workloads. Zero means an older probe with separate read and write runs.
	RandReadPercent int `json:"rand_read_percent,omitempty"`
	SeqReadPercent  int `json:"seq_read_percent,omitempty"`
	// QD1RandRead and QD1RandWrite are 4 KiB random reads and writes with one
	// I/O in flight. A client reads state mostly in this way.
	QD1RandRead   *LatencyResult `json:"qd1_rand_read,omitempty"`
	QD1RandWrite  *LatencyResult `json:"qd1_rand_write,omitempty"`
	FileSizeBytes int64          `json:"file_size_bytes"`
	IODepth       int            `json:"io_depth"`
	Duration      string         `json:"duration"`
	RandBlockSize int            `json:"rand_block_size"`
	SeqBlockSize  int            `json:"seq_block_size"`
}

// LatencyResult is the rate and the latency of a workload with one I/O in
// flight. The latency has a resolution of 1 µs.
type LatencyResult struct {
	IOPS  float64 `json:"iops"`
	P50Us float64 `json:"p50_us"`
	P99Us float64 `json:"p99_us"`
}

// workload is one measured I/O pattern.
type workload struct {
	name      string
	blockSize int64
	random    bool
	// readPercent is the share of reads: 100 reads only, 0 writes only.
	readPercent int
}

// workloadResult is the outcome of a workload.
type workloadResult struct {
	readsPerSec  float64
	writesPerSec float64
	// latency is set for a workload with one I/O in flight.
	latency *LatencyResult
}

// Probe measures the random IOPS and the sequential bandwidth of the device
// under cfg.Dir. It writes a file of cfg.FileSize bytes with direct I/O and
// removes it when it is done.
func Probe(ctx context.Context, log logrus.FieldLogger, cfg ProbeConfig) (*ProbeResult, error) {
	if !supported {
		return nil, ErrUnsupported
	}

	return probe(ctx, log, cfg, openDirect)
}

// probe runs the workloads. open creates the test file, so that tests can
// replace direct I/O, which some filesystems (tmpfs) do not support.
func probe(
	ctx context.Context,
	log logrus.FieldLogger,
	cfg ProbeConfig,
	open func(path string) (*os.File, error),
) (*ProbeResult, error) {
	cfg = withProbeDefaults(cfg)

	// Round down to whole sequential blocks, so each offset is aligned.
	size := cfg.FileSize / seqBlockSize * seqBlockSize
	if size < minProbeFileSize {
		return nil, fmt.Errorf("probe file size %d is less than the minimum of %s",
			cfg.FileSize, units.BytesSize(minProbeFileSize))
	}

	if free, err := freeBytes(cfg.Dir); err == nil && free < uint64(size)+probeFreeMargin {
		return nil, fmt.Errorf("probe needs %s free in %s but only %s is free",
			units.BytesSize(float64(size+probeFreeMargin)), cfg.Dir, units.BytesSize(float64(free)))
	}

	path := filepath.Join(cfg.Dir, fmt.Sprintf(".benchmarkoor-disk-probe-%d", time.Now().UnixNano()))

	f, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("creating the probe file: %w", err)
	}

	defer func() {
		_ = f.Close()

		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			log.WithError(rmErr).WithField("path", path).Warn("Failed to remove the disk probe file")
		}
	}()

	log.WithFields(logrus.Fields{
		"path":      path,
		"file_size": units.BytesSize(float64(size)),
		"io_depth":  cfg.IODepth,
		"duration":  cfg.Duration.String(),
	}).Info("Probing disk I/O capacity")

	// The file must be written before the reads: a read of a hole returns
	// zeros and never reaches the device.
	if err := fillFile(ctx, f, size, cfg.IODepth); err != nil {
		return nil, fmt.Errorf("writing the probe file: %w", err)
	}

	result := &ProbeResult{
		RandReadPercent: randReadPercent,
		SeqReadPercent:  seqReadPercent,
		FileSizeBytes:   size,
		IODepth:         cfg.IODepth,
		Duration:        cfg.Duration.String(),
		RandBlockSize:   randBlockSize,
		SeqBlockSize:    seqBlockSize,
	}

	// The QD1 workloads come first. They measure latency, and the deep
	// workloads can leave the drive busy with garbage collection.
	qd1Read, err := runWorkload(ctx, f, workload{"qd1_rand_read", randBlockSize, true, 100}, size, 1, cfg.Duration)
	if err != nil {
		return nil, err
	}

	qd1Write, err := runWorkload(ctx, f, workload{"qd1_rand_write", randBlockSize, true, 0}, size, 1, cfg.Duration)
	if err != nil {
		return nil, err
	}

	randMix, err := runWorkload(ctx, f,
		workload{"rand_mixed", randBlockSize, true, randReadPercent}, size, cfg.IODepth, cfg.Duration)
	if err != nil {
		return nil, err
	}

	seqMix, err := runWorkload(ctx, f,
		workload{"seq_mixed", seqBlockSize, false, seqReadPercent}, size, cfg.IODepth, cfg.Duration)
	if err != nil {
		return nil, err
	}

	result.QD1RandRead = qd1Read.latency
	result.QD1RandWrite = qd1Write.latency
	result.RandReadIOPS = randMix.readsPerSec
	result.RandWriteIOPS = randMix.writesPerSec
	result.SeqReadBps = seqMix.readsPerSec * seqBlockSize
	result.SeqWriteBps = seqMix.writesPerSec * seqBlockSize

	return result, nil
}

func withProbeDefaults(cfg ProbeConfig) ProbeConfig {
	if cfg.FileSize <= 0 {
		cfg.FileSize = DefaultProbeFileSize
	}

	if cfg.Duration <= 0 {
		cfg.Duration = DefaultProbeDuration
	}

	if cfg.IODepth <= 0 {
		cfg.IODepth = DefaultProbeIODepth
	}

	return cfg
}

// fillFile writes the whole file with random data. Random data keeps a
// compressing drive or filesystem from shrinking the test file.
func fillFile(ctx context.Context, f *os.File, size int64, depth int) error {
	blocks := size / seqBlockSize

	var next atomic.Int64

	g, ctx := errgroup.WithContext(ctx)

	for i := range depth {
		g.Go(func() error {
			buf := randomBuffer(seqBlockSize, uint64(i))

			for {
				if err := ctx.Err(); err != nil {
					return err
				}

				idx := next.Add(1) - 1
				if idx >= blocks {
					return nil
				}

				if _, err := f.WriteAt(buf, idx*seqBlockSize); err != nil {
					return err
				}
			}
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	return f.Sync()
}

// runWorkload runs depth workers for d and returns the reads and writes per
// second. Each operation is a read with a chance of w.readPercent, as fio
// does in a mixed workload. A sequential workload shares one offset counter
// for each direction between the workers, as a deep queue on one sequential
// stream does. With depth 1, it also measures the latency of each operation.
func runWorkload(
	ctx context.Context,
	f *os.File,
	w workload,
	size int64,
	depth int,
	d time.Duration,
) (*workloadResult, error) {
	blocks := size / w.blockSize
	reads := make([]int64, depth)
	writes := make([]int64, depth)

	var (
		nextRead, nextWrite atomic.Int64
		hist                *latencyHistogram
	)

	if depth == 1 {
		hist = &latencyHistogram{}
	}

	g, ctx := errgroup.WithContext(ctx)
	start := time.Now()
	deadline := start.Add(d)

	for i := range depth {
		g.Go(func() error {
			seed := uint64(start.UnixNano()) + uint64(i)
			buf := randomBuffer(int(w.blockSize), seed)
			rng := mrand.New(mrand.NewPCG(seed, uint64(i)))

			for {
				opStart := time.Now()
				if !opStart.Before(deadline) {
					return nil
				}

				if err := ctx.Err(); err != nil {
					return err
				}

				read := rng.IntN(100) < w.readPercent

				var idx int64

				switch {
				case w.random:
					idx = rng.Int64N(blocks)
				case read:
					idx = (nextRead.Add(1) - 1) % blocks
				default:
					idx = (nextWrite.Add(1) - 1) % blocks
				}

				var err error
				if read {
					_, err = f.ReadAt(buf, idx*w.blockSize)
				} else {
					_, err = f.WriteAt(buf, idx*w.blockSize)
				}

				if err != nil {
					return err
				}

				if hist != nil {
					hist.add(time.Since(opStart))
				}

				if read {
					reads[i]++
				} else {
					writes[i]++
				}
			}
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("running the %s workload: %w", w.name, err)
	}

	elapsed := time.Since(start).Seconds()

	var totalReads, totalWrites int64
	for i := range depth {
		totalReads += reads[i]
		totalWrites += writes[i]
	}

	result := &workloadResult{
		readsPerSec:  float64(totalReads) / elapsed,
		writesPerSec: float64(totalWrites) / elapsed,
	}

	if hist != nil {
		result.latency = &LatencyResult{
			IOPS:  float64(totalReads+totalWrites) / elapsed,
			P50Us: hist.percentile(0.50),
			P99Us: hist.percentile(0.99),
		}
	}

	return result, nil
}

// latencyHistogram counts latencies in 1 µs buckets up to maxLatencyUs. It
// has a fixed size, so a long probe does not grow its memory.
type latencyHistogram struct {
	buckets [maxLatencyUs + 1]uint64
	count   uint64
}

func (h *latencyHistogram) add(d time.Duration) {
	us := d.Microseconds()
	if us > maxLatencyUs {
		us = maxLatencyUs
	}

	h.buckets[us]++
	h.count++
}

// percentile returns the latency in µs below which a share p of the
// operations fall. It returns 0 when the histogram is empty.
func (h *latencyHistogram) percentile(p float64) float64 {
	if h.count == 0 {
		return 0
	}

	rank := uint64(math.Ceil(p * float64(h.count)))
	if rank == 0 {
		rank = 1
	}

	var seen uint64

	for us, n := range h.buckets {
		seen += n
		if seen >= rank {
			return float64(us)
		}
	}

	return maxLatencyUs
}

// randomBuffer returns a buffer of size random bytes, aligned for direct I/O.
func randomBuffer(size int, seed uint64) []byte {
	raw := make([]byte, size+directIOAlign)

	offset := 0
	if rem := alignOffset(raw); rem != 0 {
		offset = directIOAlign - rem
	}

	buf := raw[offset : offset+size]

	rng := mrand.NewChaCha8([32]byte{byte(seed), byte(seed >> 8), byte(seed >> 16), byte(seed >> 24)})
	_, _ = rng.Read(buf)

	return buf
}

// alignOffset returns how far the start of b is past a directIOAlign boundary.
func alignOffset(b []byte) int {
	return int(uintptr(unsafe.Pointer(&b[0])) % directIOAlign)
}
