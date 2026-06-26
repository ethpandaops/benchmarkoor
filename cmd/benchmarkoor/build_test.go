package main

import (
	"context"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/builder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
