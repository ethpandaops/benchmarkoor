package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/ethpandaops/benchmarkoor/pkg/jsonrpc"
)

const (
	testJWT = "0000000000000000000000000000000000000000000000000000000000000001"
	// rejectedNewPayload is a newPayload line whose block the client must reject.
	rejectedNewPayload = `{"jsonrpc":"2.0","id":1,"method":"engine_newPayloadV5",` +
		`"params":[{"blockHash":"0xBAD","blockNumber":"0x2","gasUsed":"0x9886df"}]}`
)

// fakeEngine answers every call with status and counts the calls it gets.
func fakeEngine(t *testing.T, status string, calls *atomic.Int32) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		calls.Add(1)

		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"status":"` + status +
			`","validationError":"invalid gas used (remote: 9995999 local: 9996000)"}}`))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func runRejectedPayload(t *testing.T, status string, opts *ExecuteOptions) (*TestResult, int32) {
	t.Helper()

	var calls atomic.Int32

	srv := fakeEngine(t, status, &calls)
	opts.EngineEndpoint = srv.URL
	opts.JWT = testJWT

	e := &executor{log: logrus.New(), validator: jsonrpc.DefaultValidator()}
	result := NewTestResult("test")
	rejections := map[string]*eest.Rejection{
		"0xbad": {ValidationError: "BlockException.INVALID_GAS_USED"},
	}

	err := e.runStepLines(context.Background(), opts, "test/test",
		newSliceLineSource([]string{rejectedNewPayload}), result, false, 0,
		StepTypeTest, rejections)
	require.NoError(t, err)

	return result, calls.Load()
}

func TestRunStepLines_RejectedPayloadPassesOnInvalid(t *testing.T) {
	result, calls := runRejectedPayload(t, "INVALID", &ExecuteOptions{})

	assert.Equal(t, int32(1), calls)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, []int{0}, result.Rejected)
	assert.Positive(t, result.Times[0])
	// The time to reject is the metric; the header gas is not.
	assert.Empty(t, result.GasUsed)
	assert.Empty(t, result.MGasPerSec)
}

func TestRunStepLines_RejectedPayloadFailsOnValidWithoutRetry(t *testing.T) {
	opts := &ExecuteOptions{
		RetryNewPayloadsFailedConfig: &config.RetryNewPayloadsFailedConfig{
			Enabled: true, MaxRetries: 3, Backoff: "1ms",
		},
	}

	result, calls := runRejectedPayload(t, "VALID", opts)

	// A client remembers a bad block, so a resend would measure nothing.
	assert.Equal(t, int32(1), calls)
	assert.Equal(t, 0, result.Succeeded)
	assert.Equal(t, 1, result.Failed)
	assert.Empty(t, result.GasUsed)
}

func TestLookupRejection(t *testing.T) {
	rejections := map[string]*eest.Rejection{"0xbad": {}}

	assert.NotNil(t, lookupRejection(rejections, "engine_newPayloadV5", rejectedNewPayload))
	assert.Nil(t, lookupRejection(rejections, "engine_forkchoiceUpdatedV4", rejectedNewPayload))
	assert.Nil(t, lookupRejection(nil, "engine_newPayloadV5", rejectedNewPayload))
	assert.Nil(t, lookupRejection(rejections, "engine_newPayloadV5",
		strings.Replace(rejectedNewPayload, "0xBAD", "0xGOOD", 1)))
}

func TestAddRejectedResult_KeepsGasOutOfStats(t *testing.T) {
	result := NewTestResult("test")
	validPayload := strings.Replace(rejectedNewPayload, "0xBAD", "0xGOOD", 1)

	result.AddResult("engine_newPayloadV5", validPayload, "", 100, true, nil)
	result.AddRejectedResult("engine_newPayloadV5", rejectedNewPayload, "", 50, true, nil)
	// A client that accepts the payload fails the call; it has no time to reject.
	result.AddRejectedResult("engine_newPayloadV5", rejectedNewPayload, "", 30, false, nil)

	stats := result.CalculateStats()

	assert.Equal(t, []int{1, 2}, result.Rejected)
	assert.Equal(t, uint64(0x9886df), stats.GasUsedTotal)
	assert.Equal(t, int64(100), stats.GasUsedTimeTotal)
	assert.Equal(t, int64(180), stats.TotalTime)
	assert.Equal(t, 1, stats.Rejected)
	assert.Equal(t, int64(50), stats.RejectedTimeTotal)
	assert.Equal(t, 2, stats.Succeeded)
	assert.Equal(t, 1, stats.Failed)
}

func TestAccumulateRunResult_CarriesRejectedTime(t *testing.T) {
	result := `{"tests": {"invalid": {"dir": "invalid", "steps": {
		"setup": {"aggregated": {"time_total": 30, "gas_used_total": 1000,
			"gas_used_time_total": 10, "success": 2, "fail": 0, "msg_count": 2}},
		"test": {"aggregated": {"time_total": 47, "gas_used_total": 0,
			"gas_used_time_total": 0, "rejected": 1, "rejected_time_total": 47,
			"success": 1, "fail": 0, "msg_count": 1}}}}}}`
	stats := make(SuiteStats, 1)

	AccumulateRunResult(&stats, []byte(result), RunInfo{RunID: "run", Client: "geth"})

	require.Contains(t, stats, "invalid")
	require.Len(t, stats["invalid"].Durations, 1)

	duration := stats["invalid"].Durations[0]
	assert.Equal(t, int64(47), duration.RejectedTime)
	assert.Equal(t, int64(47), duration.Steps.Test.RejectedTime)
	assert.Equal(t, int64(0), duration.Steps.Setup.RejectedTime)
	assert.Equal(t, uint64(1000), duration.GasUsed)
}

func TestExpectedRejections(t *testing.T) {
	gasUsed := &eest.Rejection{ValidationError: "BlockException.INVALID_GAS_USED"}
	stateRoot := &eest.Rejection{ValidationError: "BlockException.INVALID_STATE_ROOT"}

	test := &TestWithSteps{
		Setup: &StepFile{Rejections: map[string]*eest.Rejection{"0x1": stateRoot}},
		Test: &StepFile{Rejections: map[string]*eest.Rejection{
			"0x2": gasUsed,
			"0x3": gasUsed,
			"0x4": {ErrorCode: nil},
		}},
	}

	assert.Equal(t,
		[]string{"BlockException.INVALID_GAS_USED", "BlockException.INVALID_STATE_ROOT"},
		expectedRejections(test))
	assert.Nil(t, expectedRejections(&TestWithSteps{Test: &StepFile{}}))
}
