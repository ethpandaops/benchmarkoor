package generate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTxMetadata(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		want    *TxMetadata
		wantErr bool
	}{
		{
			name: "valid metadata",
			id:   `{"testId":"tests/benchmark/compute/test_add.py::test_add[fork_Prague-benchmark-gas-value_100M]","phase":"testing","txIndex":0}`,
			want: &TxMetadata{
				TestID:  "tests/benchmark/compute/test_add.py::test_add[fork_Prague-benchmark-gas-value_100M]",
				Phase:   "testing",
				TxIndex: 0,
			},
		},
		{
			name: "setup phase",
			id:   `{"testId":"tests/benchmark/compute/test_add.py::test_add","phase":"setup","txIndex":1}`,
			want: &TxMetadata{
				TestID:  "tests/benchmark/compute/test_add.py::test_add",
				Phase:   "setup",
				TxIndex: 1,
			},
		},
		{
			name:    "empty testId",
			id:      `{"testId":"","phase":"testing","txIndex":0}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			id:      `not json`,
			wantErr: true,
		},
		{
			name:    "numeric id",
			id:      `42`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := parseTxMetadata(json.RawMessage(tt.id))
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.TestID, meta.TestID)
			assert.Equal(t, tt.want.Phase, meta.Phase)
			assert.Equal(t, tt.want.TxIndex, meta.TxIndex)
		})
	}
}

func TestScenarioName(t *testing.T) {
	tests := []struct {
		name   string
		testID string
		want   string
	}{
		{
			name:   "standard format",
			testID: "tests/benchmark/compute/precompile/test_identity.py::test_identity[fork_Prague-benchmark-gas-value_100M-blockchain_test_engine_x]",
			want:   "test_identity.py__test_identity[fork_Prague-benchmark-gas-value_100M-blockchain_test_engine_x]",
		},
		{
			name:   "simple test",
			testID: "test_add.py::test_add[fork_Prague]",
			want:   "test_add.py__test_add[fork_Prague]",
		},
		{
			name:   "no separator",
			testID: "some_test_name",
			want:   "some_test_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scenarioName(tt.testID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComputeTxHash(t *testing.T) {
	// Verify hash computation produces correct-length output.
	hash := computeTxHash("0xf86c808504a817c80082520894095e7baea6a6c7c4c2dfeb977efac326af552d870a801ba048b55bfa915ac795c431978d8a6a992b628d557da5ff759b307d495a36649353a0efffd310ac743f371de3b9f7f9cb56c0b28ad43601b4ab949f53faa07bd2c804")
	assert.Equal(t, 66, len(hash)) // 0x + 64 hex chars
	assert.Equal(t, "0x", hash[:2])
}

func TestHexToByte(t *testing.T) {
	assert.Equal(t, byte(0), hexToByte('0'))
	assert.Equal(t, byte(9), hexToByte('9'))
	assert.Equal(t, byte(10), hexToByte('a'))
	assert.Equal(t, byte(15), hexToByte('f'))
	assert.Equal(t, byte(10), hexToByte('A'))
	assert.Equal(t, byte(15), hexToByte('F'))
}
