package eest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rejectionPayloadJSON is an engineNewPayloads entry shaped like EEST writes it
// for a block the client must reject; extra holds the optional fields.
func rejectionPayloadJSON(extra string) []byte {
	return []byte(`{
		"newPayloadVersion": "5",
		"forkchoiceUpdatedVersion": "4",
		"params": [{"blockHash": "0xAbC", "blockNumber": "0x2", "gasUsed": "0x9886df"}]` +
		extra + `}`)
}

func TestEngineNewPayload_UnmarshalJSON_Rejection(t *testing.T) {
	code := -32602

	tests := []struct {
		name          string
		extra         string
		wantError     string
		wantCode      *int
		wantRejection bool
		wantErr       bool
	}{
		{name: "valid payload"},
		{
			name:          "validation error string",
			extra:         `, "validationError": "BlockException.INVALID_GAS_USED"`,
			wantError:     "BlockException.INVALID_GAS_USED",
			wantRejection: true,
		},
		{
			name:          "error code as decimal string",
			extra:         `, "errorCode": "-32602"`,
			wantCode:      &code,
			wantRejection: true,
		},
		{
			name:          "error code as number",
			extra:         `, "errorCode": -32602`,
			wantCode:      &code,
			wantRejection: true,
		},
		{name: "null error code", extra: `, "errorCode": null`},
		{name: "malformed error code", extra: `, "errorCode": "abc"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload EngineNewPayload

			err := json.Unmarshal(rejectionPayloadJSON(tt.extra), &payload)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantError, payload.ValidationError)
			assert.Equal(t, tt.wantCode, payload.ErrorCode)
			assert.Equal(t, tt.wantRejection, payload.ExpectsRejection())
		})
	}
}

func TestFixtureInfo_ParsesBenchmarkGasUsed(t *testing.T) {
	var info FixtureInfo

	require.NoError(t, json.Unmarshal(
		[]byte(`{"fixture-format": "blockchain_test_stateful_engine",
			"metadata": {"benchmark_gas_used": 9996000}}`), &info))

	require.NotNil(t, info.Metadata)
	require.NotNil(t, info.Metadata.BenchmarkGasUsed)
	assert.Equal(t, uint64(9996000), *info.Metadata.BenchmarkGasUsed)
}

// payloadWithHash returns a minimal payload with the given block hash and
// parent hash.
func payloadWithHash(blockHash, parentHash string) *EngineNewPayload {
	return &EngineNewPayload{
		ExecutionPayload: &ExecutionPayload{
			BlockHash:    blockHash,
			ParentHash:   parentHash,
			BlockNumber:  "0x1",
			Transactions: []string{},
		},
		NewPayloadVersion:        5,
		ForkchoiceUpdatedVersion: 4,
		BlobVersionedHashes:      []string{},
		ExecutionRequests:        []string{},
	}
}

func methods(t *testing.T, lines []string) []string {
	t.Helper()

	out := make([]string, 0, len(lines))

	for _, line := range lines {
		var call struct {
			Method string `json:"method"`
		}

		require.NoError(t, json.Unmarshal([]byte(line), &call))

		out = append(out, call.Method)
	}

	return out
}

func TestConvertStatefulFixture_RejectedPayload(t *testing.T) {
	invalid := payloadWithHash("0xBAD", "0xsetup")
	invalid.ValidationError = "BlockException.INVALID_GAS_USED"

	fixture := &Fixture{
		Info:                   &FixtureInfo{FixtureFormat: SupportedStatefulFixtureFormat},
		SetupEngineNewPayloads: []*EngineNewPayload{payloadWithHash("0xsetup", "0xstart")},
		EngineNewPayloads:      []*EngineNewPayload{invalid},
		LastBlockHash:          "0xsetup",
	}

	result, err := ConvertStatefulFixture("test", fixture, nil)
	require.NoError(t, err)

	// The rejected payload gets no forkchoiceUpdated; the head stays on its parent.
	assert.Equal(t, []string{"engine_newPayloadV5"}, methods(t, result.TestLines))
	assert.Equal(t, "0xsetup", result.FinalHash)
	require.Contains(t, result.Rejections, "0xbad")
	assert.Equal(t, "BlockException.INVALID_GAS_USED", result.Rejections["0xbad"].ValidationError)
	assert.Nil(t, result.Rejections["0xbad"].ErrorCode)
	assert.Equal(t, "0xsetup", result.Rejections["0xbad"].ParentHash)
}

func TestConvertFixture_RejectedPayloadKeepsHeadOnLastValidBlock(t *testing.T) {
	invalid := payloadWithHash("0xbad", "0xvalid")
	invalid.ValidationError = "BlockException.INVALID_GAS_USED"

	fixture := &Fixture{
		EngineNewPayloads: []*EngineNewPayload{payloadWithHash("0xvalid", "0xgenesis"), invalid},
	}

	result, err := ConvertFixture("test", fixture)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"engine_newPayloadV5", "engine_forkchoiceUpdatedV4"}, methods(t, result.SetupLines))
	assert.Equal(t, []string{"engine_newPayloadV5"}, methods(t, result.TestLines))
	assert.Equal(t, "0xvalid", result.FinalHash)
	assert.Len(t, result.Rejections, 1)
}

func TestConvertFixture_ValidFixtureHasNoRejections(t *testing.T) {
	fixture := &Fixture{
		EngineNewPayloads: []*EngineNewPayload{payloadWithHash("0xvalid", "0xgenesis")},
	}

	result, err := ConvertFixture("test", fixture)
	require.NoError(t, err)

	assert.Nil(t, result.Rejections)
	assert.Equal(t, "0xvalid", result.FinalHash)
}
