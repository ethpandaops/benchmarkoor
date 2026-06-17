package stats

import (
	"sync"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestReader builds a dockerReader without starting the streaming goroutine,
// so the cache logic can be exercised without a Docker daemon.
func newTestReader() *dockerReader {
	return &dockerReader{
		log:   logrus.New(),
		ready: make(chan struct{}),
	}
}

func TestDockerReader_ReadStatsBeforeFirstSample(t *testing.T) {
	r := newTestReader()

	_, err := r.ReadStats()
	require.Error(t, err, "ReadStats must error until the first sample is cached")
}

func TestDockerReader_UpdateCachesSampleAndSignalsReady(t *testing.T) {
	r := newTestReader()

	ds := &container.StatsResponse{}
	ds.MemoryStats.Usage = 2048
	ds.CPUStats.CPUUsage.TotalUsage = 5000 // nanoseconds
	ds.BlkioStats.IoServiceBytesRecursive = []container.BlkioStatEntry{
		{Op: "Read", Value: 100},
		{Op: "Write", Value: 200},
	}
	ds.BlkioStats.IoServicedRecursive = []container.BlkioStatEntry{
		{Op: "Read", Value: 3},
		{Op: "Write", Value: 4},
	}

	r.update(ds)

	// readyOnce should have closed the ready channel.
	select {
	case <-r.ready:
	default:
		t.Fatal("ready channel was not closed after first update")
	}

	got, err := r.ReadStats()
	require.NoError(t, err)
	assert.Equal(t, uint64(2048), got.Memory)
	assert.Equal(t, uint64(5), got.CPUUsage, "nanoseconds should convert to microseconds")
	assert.Equal(t, uint64(100), got.DiskRead)
	assert.Equal(t, uint64(200), got.DiskWrite)
	assert.Equal(t, uint64(3), got.DiskReadOps)
	assert.Equal(t, uint64(4), got.DiskWriteOps)
}

func TestDockerReader_ReadStatsReturnsCopy(t *testing.T) {
	r := newTestReader()

	ds := &container.StatsResponse{}
	ds.MemoryStats.Usage = 1000
	r.update(ds)

	first, err := r.ReadStats()
	require.NoError(t, err)

	// Mutating the returned snapshot must not affect the cached sample.
	first.Memory = 9999

	second, err := r.ReadStats()
	require.NoError(t, err)
	assert.Equal(t, uint64(1000), second.Memory, "cached sample must be isolated from callers")
}

func TestDockerReader_UpdateReadConcurrent(t *testing.T) {
	r := newTestReader()

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := range 1000 {
			ds := &container.StatsResponse{}
			ds.MemoryStats.Usage = uint64(i)
			r.update(ds)
		}
	}()

	go func() {
		defer wg.Done()

		for range 1000 {
			_, _ = r.ReadStats()
		}
	}()

	wg.Wait()
}
