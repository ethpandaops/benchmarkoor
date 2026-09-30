//go:build linux

package blockdev

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux native AIO opcodes (include/uapi/linux/aio_abi.h).
const (
	iocbCmdPread  = 0
	iocbCmdPwrite = 1
)

// aioPollTimeout bounds each io_getevents call, so the loop sees the
// deadline and a cancelled context while I/O is in flight.
const aioPollTimeout = 100 * time.Millisecond

// iocb is struct iocb of the Linux AIO ABI (64 bytes). The probe leaves
// aio_key and aio_rw_flags at zero, so their order does not depend on the
// byte order.
type iocb struct {
	data   uint64
	_      uint32 // aio_key
	_      uint32 // aio_rw_flags
	opcode uint16
	_      int16 // aio_reqprio
	fildes uint32
	buf    uint64
	nbytes uint64
	offset int64
	_      uint64 // aio_reserved2
	_      uint32 // aio_flags
	_      uint32 // aio_resfd
}

// ioEvent is struct io_event of the Linux AIO ABI (32 bytes).
type ioEvent struct {
	data uint64
	_    uint64 // obj
	res  int64
	_    int64 // res2
}

// The kernel reads and writes these structs, so their sizes must match.
var (
	_ [64 - unsafe.Sizeof(iocb{})]struct{}
	_ [unsafe.Sizeof(iocb{}) - 64]struct{}
	_ [32 - unsafe.Sizeof(ioEvent{})]struct{}
	_ [unsafe.Sizeof(ioEvent{}) - 32]struct{}
)

// runDeepWorkload runs a workload as fio --ioengine=libaio with one job does:
// one thread keeps depth operations in flight with Linux native AIO, and it
// submits one operation for each io_submit call. The EIP-7870 fio commands
// use this method, so the CPU time of the thread can limit the result on a
// fast drive. The workload stops after w.ioBytes, or after d.
func runDeepWorkload(
	ctx context.Context,
	f *os.File,
	w workload,
	size int64,
	depth int,
	d time.Duration,
) (*workloadResult, error) {
	res, err := runAIOWorkload(ctx, f, w, size, depth, d)
	if err != nil {
		return nil, fmt.Errorf("running the %s workload: %w", w.name, err)
	}

	return res, nil
}

func runAIOWorkload(
	ctx context.Context,
	f *os.File,
	w workload,
	size int64,
	depth int,
	d time.Duration,
) (*workloadResult, error) {
	var aioCtx uint64
	if _, _, errno := unix.Syscall(unix.SYS_IO_SETUP, uintptr(depth), uintptr(unsafe.Pointer(&aioCtx)), 0); errno != 0 {
		return nil, fmt.Errorf("io_setup: %w", errno)
	}

	start := time.Now()
	deadline := start.Add(d)
	seed := uint64(start.UnixNano())
	rng := mrand.New(mrand.NewPCG(seed, 0))
	blocks := size / w.blockSize

	// One control block and one buffer for each slot. The kernel reads the
	// blocks at submit time and uses the buffers until the I/O completes.
	cbs := make([]iocb, depth)
	bufs := make([][]byte, depth)
	pending := make([]*iocb, 0, depth)
	events := make([]ioEvent, depth)

	// io_destroy waits for the I/O in flight, which still uses the control
	// blocks and the buffers. Thus they must stay alive until it returns.
	defer func() {
		_, _, _ = unix.Syscall(unix.SYS_IO_DESTROY, uintptr(aioCtx), 0, 0)

		runtime.KeepAlive(cbs)
		runtime.KeepAlive(bufs)
	}()

	var nextRead, nextWrite, reads, writes, issued int64

	// done tells if the workload has issued its ioBytes.
	done := func() bool { return w.ioBytes > 0 && issued >= w.ioBytes }

	prepare := func(slot int) {
		issued += w.blockSize

		read := rng.IntN(100) < w.readPercent

		var idx int64

		switch {
		case w.random:
			idx = rng.Int64N(blocks)
		case read:
			idx = nextRead % blocks
			nextRead++
		default:
			idx = nextWrite % blocks
			nextWrite++
		}

		cb := &cbs[slot]
		*cb = iocb{
			data:   uint64(slot),
			opcode: iocbCmdPwrite,
			fildes: uint32(f.Fd()),
			buf:    uint64(uintptr(unsafe.Pointer(&bufs[slot][0]))),
			nbytes: uint64(w.blockSize),
			offset: idx * w.blockSize,
		}

		if read {
			cb.opcode = iocbCmdPread
		}

		pending = append(pending, cb)
	}

	for slot := range depth {
		bufs[slot] = randomBuffer(int(w.blockSize), seed+uint64(slot))

		if !done() {
			prepare(slot)
		}
	}

	var (
		inflight int
		runErr   error
	)

	for {
		stopping := runErr != nil || ctx.Err() != nil || !time.Now().Before(deadline)
		if stopping {
			pending = pending[:0]
		}

		// One I/O for each io_submit call, as fio does by default
		// (iodepth_batch_submit=1). A batch would use less CPU for each I/O
		// and give a higher result than the EIP-7870 command.
		for len(pending) > 0 {
			n, _, errno := unix.Syscall(unix.SYS_IO_SUBMIT, uintptr(aioCtx),
				1, uintptr(unsafe.Pointer(&pending[0])))
			if (errno == unix.EINTR || errno == unix.EAGAIN) && inflight > 0 {
				break
			}

			if errno == 0 && n == 0 {
				errno = unix.EAGAIN
			}

			if errno != 0 {
				runErr = fmt.Errorf("io_submit: %w", errno)
				pending = pending[:0]

				break
			}

			inflight += int(n)
			pending = pending[:copy(pending, pending[n:])]
		}

		if inflight == 0 {
			break
		}

		timeout := unix.NsecToTimespec(aioPollTimeout.Nanoseconds())

		n, _, errno := unix.Syscall6(unix.SYS_IO_GETEVENTS, uintptr(aioCtx), 1, uintptr(depth),
			uintptr(unsafe.Pointer(&events[0])), uintptr(unsafe.Pointer(&timeout)), 0)
		if errno == unix.EINTR {
			continue
		}

		if errno != 0 {
			// The deferred io_destroy waits for the I/O in flight.
			return nil, fmt.Errorf("io_getevents: %w", errno)
		}

		stopping = runErr != nil || ctx.Err() != nil || !time.Now().Before(deadline)

		for _, ev := range events[:n] {
			inflight--

			slot := int(ev.data)
			if ev.res < 0 {
				runErr = errors.Join(runErr, fmt.Errorf("aio: %w", unix.Errno(-ev.res)))

				continue
			}

			if ev.res != int64(cbs[slot].nbytes) {
				runErr = errors.Join(runErr, fmt.Errorf("aio: short transfer of %d of %d bytes", ev.res, cbs[slot].nbytes))

				continue
			}

			if cbs[slot].opcode == iocbCmdPread {
				reads++
			} else {
				writes++
			}

			if !stopping && !done() {
				prepare(slot)
			}
		}
	}

	elapsed := time.Since(start).Seconds()

	if runErr != nil {
		return nil, runErr
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &workloadResult{
		readsPerSec:  float64(reads) / elapsed,
		writesPerSec: float64(writes) / elapsed,
	}, nil
}
