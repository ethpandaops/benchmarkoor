package builder

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHexRoundTrip(t *testing.T) {
	cases := []uint64{0, 1, 15, 16, 255, 256, 1_000_000_000_000, 1<<64 - 1}
	for _, v := range cases {
		got, err := hexToUint64(uintToHex(v))
		require.NoError(t, err)
		assert.Equal(t, v, got)
	}

	// Accepts a bare (unprefixed) hex string too.
	got, err := hexToUint64("ff")
	require.NoError(t, err)
	assert.Equal(t, uint64(255), got)
}

func TestNewEngineClient_NewPayloadMethod(t *testing.T) {
	// 32-byte hex JWT secret.
	jwt := strings.Repeat("ab", 32)

	amsterdam, err := newEngineClient("10.0.0.1", 8545, 8551, jwt, "amsterdam")
	require.NoError(t, err)
	assert.Equal(t, "engine_newPayloadV5", amsterdam.newPayloadMethod())

	osaka, err := newEngineClient("10.0.0.1", 8545, 8551, "0x"+jwt, "osaka")
	require.NoError(t, err)
	assert.Equal(t, "engine_newPayloadV4", osaka.newPayloadMethod())

	assert.Equal(t, "http://10.0.0.1:8545", osaka.rpcURL)
	assert.Equal(t, "http://10.0.0.1:8551", osaka.engineURL)
}

func TestNewPayloadMethodFor(t *testing.T) {
	assert.Equal(t, "engine_newPayloadV5", newPayloadMethodFor("amsterdam"))
	assert.Equal(t, "engine_newPayloadV5", newPayloadMethodFor("Amsterdam"))
	assert.Equal(t, "engine_newPayloadV4", newPayloadMethodFor("osaka"))
	assert.Equal(t, "engine_newPayloadV4", newPayloadMethodFor("prague"))
}

func TestEngineClient_ForkAt(t *testing.T) {
	jwt := strings.Repeat("ab", 32)

	t.Run("crossing disabled always returns c.fork", func(t *testing.T) {
		c, err := newEngineClient("10.0.0.1", 8545, 8551, jwt, "amsterdam")
		require.NoError(t, err)
		// activationTS 0 (unset) → always the target fork, regardless of timestamp.
		assert.Equal(t, "amsterdam", c.forkAt(0))
		assert.Equal(t, "amsterdam", c.forkAt(1<<62))
	})

	t.Run("crossing selects preFork below activation, fork at/after", func(t *testing.T) {
		c, err := newEngineClient("10.0.0.1", 8545, 8551, jwt, "amsterdam")
		require.NoError(t, err)
		c.withCrossing("osaka", 1000)

		assert.Equal(t, "osaka", c.forkAt(999), "just below activation → pre-fork")
		assert.Equal(t, "amsterdam", c.forkAt(1000), "at activation → target fork")
		assert.Equal(t, "amsterdam", c.forkAt(1001), "above activation → target fork")

		// The per-block newPayload version follows the selected fork.
		assert.Equal(t, "engine_newPayloadV4", newPayloadMethodFor(c.forkAt(999)))
		assert.Equal(t, "engine_newPayloadV5", newPayloadMethodFor(c.forkAt(1000)))
	})

	t.Run("empty preFork disables crossing even with activationTS", func(t *testing.T) {
		c, err := newEngineClient("10.0.0.1", 8545, 8551, jwt, "amsterdam")
		require.NoError(t, err)
		c.withCrossing("", 1000)
		assert.Equal(t, "amsterdam", c.forkAt(1))
	})
}

func TestNewEngineClient_RejectsBadJWT(t *testing.T) {
	_, err := newEngineClient("10.0.0.1", 8545, 8551, "", "osaka")
	require.ErrorContains(t, err, "empty")

	_, err = newEngineClient("10.0.0.1", 8545, 8551, "nothex!!", "osaka")
	require.Error(t, err)
}

func TestMakeJWT_Verifies(t *testing.T) {
	jwt := strings.Repeat("cd", 32)
	c, err := newEngineClient("10.0.0.1", 8545, 8551, jwt, "amsterdam")
	require.NoError(t, err)

	token, err := c.makeJWT()
	require.NoError(t, err)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "JWT has header.payload.signature")

	// The signature must be a valid HS256 MAC over header.payload with the secret.
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, c.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	assert.Equal(t, want, parts[2])

	// Header decodes to the expected HS256 alg.
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	assert.Contains(t, string(header), "HS256")
}

func TestPreRunToEESTTarget(t *testing.T) {
	src := &config.PreRunTarget{
		Name:               "pre-run-geth",
		FillerClient:       "geth",
		Fork:               "amsterdam",
		Tests:              []string{"tests/benchmark/stateful/bloatnet/test_setup_contracts.py"},
		GasBenchmarkValues: []int{30000},
		FillerExtraArgs:    []string{"--override.amsterdam=1"},
	}

	et := preRunToEESTTarget(src, "/prerun/geth", "/tmp/fixtures")
	assert.Equal(t, "/prerun/geth", et.SourceDir, "filler boots on the output datadir")
	assert.Equal(t, "/tmp/fixtures", et.OutputDir)
	assert.Equal(t, "direct", et.DataDirMethod, "boots in place so writes persist")
	assert.Equal(t, "geth", et.FillerClient)
	assert.Equal(t, "amsterdam", et.Fork)
	assert.Equal(t, []string{"tests/benchmark/stateful/bloatnet/test_setup_contracts.py"}, et.Tests)
	assert.Equal(t, []int{30000}, et.GasBenchmarkValues)
	// The pre-run's own EOA-start default must reach the fill, so the setup
	// accounts stay clear of the eest_payloads fill on the same datadir.
	assert.Equal(t, config.DefaultPreRunEOAStart, et.ResolveEOAStart())

	pinned := preRunToEESTTarget(&config.PreRunTarget{EOAStart: u64(7)}, "/prerun/geth", "/tmp/fixtures")
	assert.Equal(t, uint64(7), pinned.ResolveEOAStart(), "per-target eoa_start wins")
}
