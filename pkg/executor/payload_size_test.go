package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractNewPayloadLines(t *testing.T) {
	lines := []string{
		`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[{"blockNumber":"0x1"}],"id":1}`,
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[{},null],"id":2}`,
		`{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[{"blockNumber":"0x2"}],"id":3}`,
	}
	got := ExtractNewPayloadLines(lines)
	assert.Len(t, got, 2)
	assert.Equal(t, 3, got[0].Version)
	assert.Equal(t, 4, got[1].Version)
}

func TestExtractNewPayloadLines_IgnoresMalformed(t *testing.T) {
	lines := []string{
		`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[{}],"id":1}`,
		`not even json`,
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[],"id":2}`,
	}
	got := ExtractNewPayloadLines(lines)
	assert.Len(t, got, 1)
}

func TestExtractNewPayloadLines_NoLines(t *testing.T) {
	got := ExtractNewPayloadLines(nil)
	assert.Empty(t, got)
}
