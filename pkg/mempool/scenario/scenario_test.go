package scenario

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

func TestParamsMerge(t *testing.T) {
	defaults := Params{"a": 1, "b": "250ms"}

	got, err := Params{"a": 5.0}.Merge(defaults)
	require.NoError(t, err)
	assert.Equal(t, 5, got.Int("a"))
	assert.Equal(t, 250*time.Millisecond, got.Duration("b"))

	_, err = Params{"unknown": 1}.Merge(defaults)
	require.Error(t, err)
}

func TestRegistryDefaultsAreValid(t *testing.T) {
	r := NewRegistry()
	require.Len(t, r.Names(), 14)

	for _, name := range r.Names() {
		s, err := r.Get(name)
		require.NoError(t, err)

		p, err := Params{}.Merge(s.Defaults())
		require.NoError(t, err, name)
		assert.Positive(t, s.Accounts(p), name)
		assert.NotEmpty(t, s.Description(), name)
	}

	_, err := r.Get("nope")
	require.Error(t, err)
}

func TestSummarize(t *testing.T) {
	s := &latencySeries{label: "x"}
	for i := 1; i <= 100; i++ {
		s.samples = append(s.samples, float64(i))
	}

	l := summarize("k", s)
	assert.Equal(t, 100, l.Count)
	assert.InDelta(t, 1.0, l.Min, 0)
	assert.InDelta(t, 100.0, l.Max, 0)
	assert.InDelta(t, 50.0, l.P50, 0)
	assert.InDelta(t, 90.0, l.P90, 0)
	assert.InDelta(t, 99.0, l.P99, 0)
	assert.InDelta(t, 50.5, l.Mean, 0)

	// Buckets are not cumulative: their counts add up to the sample count.
	total := 0
	for _, b := range l.Histogram {
		total += b.Count
	}

	assert.Equal(t, 100, total)
	assert.Equal(t, 1.0, l.Histogram[0].Le, "leading empty buckets are dropped")
}

func TestRecorderStatus(t *testing.T) {
	tests := []struct {
		name   string
		checks []Assertion
		err    error
		want   Status
	}{
		{"all pass", []Assertion{{Severity: SeverityHard, Passed: true}}, nil, StatusPassed},
		{"soft fail is a finding", []Assertion{{Severity: SeveritySoft, Passed: false}}, nil, StatusPassed},
		{"hard fail fails", []Assertion{{Severity: SeverityHard, Passed: false}}, nil, StatusFailed},
		{"error wins", []Assertion{{Severity: SeverityHard, Passed: false}}, assert.AnError, StatusError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRecorder("s", "d", nil)
			for _, a := range tc.checks {
				r.Check(a.Severity, a.Passed, "c", "", "")
			}

			res := r.Finish(tc.err, nil)
			assert.Equal(t, tc.want, res.Status)
		})
	}
}

func TestOrderingChecks(t *testing.T) {
	f := txfactory.New(1337, "test", 4)
	accs, err := f.Accounts(0, 2)
	require.NoError(t, err)

	tip := func(gwei int64) txfactory.Fees {
		return txfactory.Fees{FeeCap: txfactory.GweiN(100), TipCap: txfactory.GweiN(gwei)}
	}

	build := func(a *txfactory.Account, gwei int64) *types.Transaction {
		tx, err := f.Build(a, txfactory.Template{Kind: txfactory.KindDynamicFee, Fees: tip(gwei)})
		require.NoError(t, err)

		return tx
	}

	// Sender 0: tips 1, 5. Sender 1: tips 3, 2.
	a0, a1 := build(accs[0], 1), build(accs[0], 5)
	b0, b1 := build(accs[1], 3), build(accs[1], 2)

	got := greedyOrder([]*types.Transaction{a0, a1, b0, b1}, big.NewInt(1), f.Signer())
	// b0 (3) beats a0 (1); then b1 (2) beats a0 (1); then a0, a1.
	assert.Equal(t, Hashes([]*types.Transaction{b0, b1, a0, a1}), Hashes(got))

	assert.Empty(t, nonceOrderErrors([]*types.Transaction{b0, a0, b1, a1}, f.Signer()))
	assert.Len(t, nonceOrderErrors([]*types.Transaction{a1, a0}, f.Signer()), 1)

	assert.Equal(t, -1, firstMismatch(got, got))
	assert.Equal(t, 0, firstMismatch([]*types.Transaction{a0}, got))
}

func TestBlobBaseFee(t *testing.T) {
	assert.Equal(t, int64(1), blobBaseFee(0, 5007716).Int64())
	// e^(excess/fraction), rounded down: 3.48M excess is just past ln(2).
	assert.Equal(t, int64(2), blobBaseFee(3_480_000, 5007716).Int64())
	assert.Equal(t, int64(7), blobBaseFee(10_000_000, 5007716).Int64())
}
