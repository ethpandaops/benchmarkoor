package main

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/builder"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain initializes the package-level logger (nil until main() runs) so tests
// that exercise functions which log (e.g. summarise) don't nil-panic.
func TestMain(m *testing.M) {
	log = logrus.New()
	log.SetOutput(io.Discard)

	os.Exit(m.Run())
}

// fakeBuilder is a minimal builder.Builder for exercising selectTargets.
type fakeBuilder struct {
	name    string
	targets []builder.TargetInfo
}

func (f *fakeBuilder) Name() string { return f.name }

func (f *fakeBuilder) Targets() []builder.TargetInfo { return f.targets }

func (f *fakeBuilder) Build(context.Context, string, builder.BuildOptions) (bool, error) {
	return false, nil
}

func TestSummarise(t *testing.T) {
	t.Run("all OK/SKIP returns no error", func(t *testing.T) {
		err := summarise([]buildResult{
			{builder: "state-actor", name: "geth", skipped: false},
			{builder: "state-actor", name: "reth", skipped: true},
			{builder: "eest-payloads", name: "fill-geth", skipped: false},
		})
		assert.NoError(t, err)
	})

	t.Run("any failure aggregates into an error naming the failed targets", func(t *testing.T) {
		err := summarise([]buildResult{
			{builder: "state-actor", name: "geth"},
			{builder: "eest-payloads", name: "fill-besu", err: errors.New("boom")},
			{builder: "eest-payloads", name: "fill-reth", err: errors.New("kaboom")},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "2 target(s) failed")
		assert.Contains(t, err.Error(), "fill-besu")
		assert.Contains(t, err.Error(), "fill-reth")
		// A successful target must not appear in the failed list.
		assert.NotContains(t, err.Error(), "geth")
	})
}

func TestLimitFilters(t *testing.T) {
	// limitFilters reads the package-level flag vars; save and restore them.
	savedSA, savedEEST := buildStateActorTargets, buildEESTPayloadTargets
	t.Cleanup(func() { buildStateActorTargets, buildEESTPayloadTargets = savedSA, savedEEST })

	buildStateActorTargets = []string{"nethermind"}
	buildEESTPayloadTargets = []string{"payload-generator-nethermind"}

	sa := &fakeBuilder{name: builder.StateActorBuilderName}
	eest := &fakeBuilder{name: builder.EESTPayloadsBuilderName}

	t.Run("both present yields both filters", func(t *testing.T) {
		got := limitFilters([]builder.Builder{sa, eest})
		require.Len(t, got, 2)
		assert.Equal(t, []string{"nethermind"}, got[builder.StateActorBuilderName].values)
		assert.Equal(t, []string{"payload-generator-nethermind"}, got[builder.EESTPayloadsBuilderName].values)
	})

	t.Run("absent (skipped) builder's limit is dropped", func(t *testing.T) {
		// state_actor skipped → only eest builder present.
		got := limitFilters([]builder.Builder{eest})
		require.Len(t, got, 1)
		_, hasSA := got[builder.StateActorBuilderName]
		assert.False(t, hasSA, "skipped builder's limit must be omitted")
		assert.Equal(t, []string{"payload-generator-nethermind"}, got[builder.EESTPayloadsBuilderName].values)
	})
}

func TestSelectTargets(t *testing.T) {
	// state_actor builds per-client snapshots; eest_payloads fills per named target.
	stateActor := &fakeBuilder{
		name: builder.StateActorBuilderName,
		targets: []builder.TargetInfo{
			{Name: "geth", Client: "geth"},
			{Name: "nethermind", Client: "nethermind"},
		},
	}
	eest := &fakeBuilder{
		name: builder.EESTPayloadsBuilderName,
		targets: []builder.TargetInfo{
			{Name: "payload-generator-geth", Client: "geth"},
			{Name: "payload-generator-nethermind", Client: "nethermind"},
		},
	}
	builders := []builder.Builder{stateActor, eest}

	filters := func(sa, eestF []string) map[string]builderFilter {
		return map[string]builderFilter{
			builder.StateActorBuilderName:   {flag: "--limit-state-actor-target", values: sa},
			builder.EESTPayloadsBuilderName: {flag: "--limit-eest-payload-target", values: eestF},
		}
	}

	tests := []struct {
		name      string
		global    []string
		sa        []string
		eest      []string
		want      []string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "no filters selects every target",
			want: []string{"geth", "nethermind", "payload-generator-geth", "payload-generator-nethermind"},
		},
		{
			name:   "global --target filters across builders",
			global: []string{"nethermind", "payload-generator-nethermind"},
			want:   []string{"nethermind", "payload-generator-nethermind"},
		},
		{
			name: "limit-eest-payload-target restricts only eest; state_actor unrestricted",
			eest: []string{"payload-generator-nethermind"},
			want: []string{"geth", "nethermind", "payload-generator-nethermind"},
		},
		{
			name: "limit-state-actor-target restricts only state_actor; eest unrestricted",
			sa:   []string{"nethermind"},
			want: []string{"nethermind", "payload-generator-geth", "payload-generator-nethermind"},
		},
		{
			name: "both per-builder filters narrow each builder",
			sa:   []string{"nethermind"},
			eest: []string{"payload-generator-nethermind"},
			want: []string{"nethermind", "payload-generator-nethermind"},
		},
		{
			name:   "global and per-builder compose (intersection)",
			global: []string{"nethermind", "payload-generator-geth", "payload-generator-nethermind"},
			eest:   []string{"payload-generator-nethermind"},
			want:   []string{"nethermind", "payload-generator-nethermind"},
		},
		{
			name:      "unknown global target errors",
			global:    []string{"besu"},
			wantErr:   true,
			errSubstr: "--target filter matched no targets: besu",
		},
		{
			name:      "unknown state-actor target errors with builder-scoped message",
			sa:        []string{"besu"},
			wantErr:   true,
			errSubstr: "--limit-state-actor-target matched no state-actor targets: besu",
		},
		{
			name:      "eest name given to state-actor filter errors (scoped per builder)",
			sa:        []string{"payload-generator-nethermind"},
			wantErr:   true,
			errSubstr: "--limit-state-actor-target matched no state-actor targets: payload-generator-nethermind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectTargets(builders, tt.global, filters(tt.sa, tt.eest))

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)

				return
			}

			require.NoError(t, err)

			names := make([]string, 0, len(got))
			for _, sel := range got {
				names = append(names, sel.info.Name)
			}

			assert.Equal(t, tt.want, names)
		})
	}
}
