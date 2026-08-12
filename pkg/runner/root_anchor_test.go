package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeChain answers eth_getBlockByNumber for "latest", "finalized" and hex
// numbers. A tag mapped to 0 is reported as a null result, the way a client
// with no finalized block answers.
type fakeChain struct {
	latest    uint64
	finalized uint64 // 0 means "no finalized block"
}

func (f fakeChain) server(t *testing.T) *httptest.Server {
	t.Helper()

	hash := func(n uint64) string {
		return fmt.Sprintf("0x%064x", n)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params []any `json:"params"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		tag, _ := req.Params[0].(string)

		var num uint64

		switch tag {
		case "latest":
			num = f.latest
		case "finalized":
			num = f.finalized
		default:
			parsed, err := strconv.ParseUint(strings.TrimPrefix(tag, "0x"), 16, 64)
			require.NoError(t, err)
			num = parsed
		}

		if num == 0 {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`))

			return
		}

		_, _ = fmt.Fprintf(w,
			`{"jsonrpc":"2.0","id":1,"result":{"number":"0x%x","hash":%q,"stateRoot":%q}}`,
			num, hash(num), hash(num))
	}))
}

func hostPort(t *testing.T, srv *httptest.Server) (string, int) {
	t.Helper()

	host, port, err := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	require.True(t, err)

	p, convErr := strconv.Atoi(port)
	require.NoError(t, convErr)

	return host, p
}

// The root anchor must land strictly below the head — the block every fixture
// replays from — since a client will not move its head back to a block at or
// below the one it considers finalized.
func TestResolveRootAnchor(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	tests := []struct {
		name       string
		chain      fakeChain
		configured string
		want       string
		reason     string
	}{
		{
			name:       "configured wins",
			chain:      fakeChain{latest: 200, finalized: 100},
			configured: "0xdeadbeef",
			want:       "0xdeadbeef",
			reason:     "an explicit hash is used verbatim, without consulting the client",
		},
		{
			name:   "reuses an already-low finalized block",
			chain:  fakeChain{latest: 200, finalized: 100},
			want:   fmt.Sprintf("0x%064x", 100),
			reason: "a finalized block below the head is already a usable anchor",
		},
		{
			name:   "falls back when finalized is the head itself",
			chain:  fakeChain{latest: 200, finalized: 200},
			want:   fmt.Sprintf("0x%064x", 199),
			reason: "this is the broken datadir: keeping it would pin the replay anchor",
		},
		{
			name:   "falls back when finalized is above the head",
			chain:  fakeChain{latest: 200, finalized: 250},
			want:   fmt.Sprintf("0x%064x", 199),
			reason: "a finalized block above the head is never a valid anchor",
		},
		{
			name:   "falls back when there is no finalized block",
			chain:  fakeChain{latest: 200, finalized: 0},
			want:   fmt.Sprintf("0x%064x", 199),
			reason: "a client that has never finalized still needs an anchor",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := tc.chain.server(t)
			defer srv.Close()

			host, port := hostPort(t, srv)
			r := &runner{}

			got, err := r.resolveRootAnchor(
				context.Background(), log, host, port, tc.configured)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, tc.reason)
		})
	}
}

// Genesis has nothing below it: the caller must be told, not handed zero.
func TestResolveRootAnchorAtGenesis(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	srv := fakeChain{latest: 0, finalized: 0}.server(t)
	defer srv.Close()

	host, port := hostPort(t, srv)
	r := &runner{}

	_, err := r.resolveRootAnchor(context.Background(), log, host, port, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "genesis")
}
