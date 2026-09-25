package indexstore

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// TestReadPoolPragmas checks that every connection in the read pool carries
// the pragmas, not only the one that happened to serve the pragma statement.
//
// This is the failure that took the admin database report and the suite stats
// endpoint down in production. SQLite applies these pragmas per connection,
// so setting them once through GORM left three of the four read connections
// on the defaults. Those three spilled the scratch b-tree of a GROUP BY to a
// temp file, which a container with a read-only root filesystem cannot write,
// and read an 86GB database through a 2MB page cache.
func TestReadPoolPragmas(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	s := NewStore(log, &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{
			Path: filepath.Join(t.TempDir(), "index.db"),
		},
	}).(*store)

	require.NoError(t, s.Start(t.Context()))
	t.Cleanup(func() { _ = s.Stop() })

	readSQL, err := s.readDB.DB()
	require.NoError(t, err)

	// Hold every connection at once, so the pool has to open the ones it
	// never opened while the store was starting up.
	const poolSize = 4

	var (
		wg    sync.WaitGroup
		ready = make(chan struct{})
		mu    sync.Mutex
		seen  []map[string]int64
	)

	for range poolSize {
		wg.Add(1)

		go func() {
			defer wg.Done()

			conn, err := readSQL.Conn(t.Context())
			if !assert.NoError(t, err) {
				return
			}

			defer func() { _ = conn.Close() }()

			<-ready

			values := readPragmas(t, conn)

			mu.Lock()
			seen = append(seen, values)
			mu.Unlock()
		}()
	}

	close(ready)
	wg.Wait()

	require.Len(t, seen, poolSize)

	for i, values := range seen {
		assert.Equal(t, int64(2), values["temp_store"],
			"connection %d must keep temp files in memory", i)
		assert.Equal(t, int64(-64000), values["cache_size"],
			"connection %d must have the configured page cache", i)
		assert.Equal(t, int64(5000), values["busy_timeout"],
			"connection %d must wait out a locked database", i)
		assert.Positive(t, values["mmap_size"],
			"connection %d must have mmap enabled", i)
	}
}

func readPragmas(t *testing.T, conn *sql.Conn) map[string]int64 {
	t.Helper()

	names := []string{"temp_store", "cache_size", "busy_timeout", "mmap_size"}
	values := make(map[string]int64, len(names))

	for _, name := range names {
		var value int64

		require.NoError(t, conn.QueryRowContext(
			t.Context(), "PRAGMA "+name,
		).Scan(&value))

		values[name] = value
	}

	return values
}
