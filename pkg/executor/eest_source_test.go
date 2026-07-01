package executor

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/stretchr/testify/assert"
)

func TestStatefulPreRunMissing(t *testing.T) {
	tests := []struct {
		name      string
		startHash string
		snapHash  string
		want      bool
	}{
		{
			name:      "start ahead of snapshot warns",
			startHash: "0xstart",
			snapHash:  "0xsnapshot",
			want:      true,
		},
		{
			name:      "start equals snapshot is silent",
			startHash: "0xsnapshot",
			snapHash:  "0xsnapshot",
			want:      false,
		},
		{
			name:      "empty start block is silent",
			startHash: "",
			snapHash:  "0xsnapshot",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &eest.Fixture{StartBlockHash: tt.startHash, SnapshotBlockHash: tt.snapHash}
			assert.Equal(t, tt.want, statefulPreRunMissing(f))
		})
	}
}
