package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateBlockOpcodes_SumsAcrossTransactions(t *testing.T) {
	resp := `{
		"jsonrpc": "2.0",
		"id": 1,
		"result": [
			{"txHash": "0xaaa", "result": {"PUSH1": 10, "MSTORE": 2, "STOP": 1}},
			{"txHash": "0xbbb", "result": {"PUSH1": 5,  "DUP1":   3, "STOP": 1}}
		]
	}`

	got, err := aggregateBlockOpcodes(resp)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{
		"PUSH1":  15,
		"MSTORE": 2,
		"STOP":   2,
		"DUP1":   3,
	}, got)
}

func TestAggregateBlockOpcodes_UppercasesNames(t *testing.T) {
	resp := `{"result":[{"txHash":"0xa","result":{"push1":4,"Push1":1,"PUSH1":2}}]}`

	got, err := aggregateBlockOpcodes(resp)
	require.NoError(t, err)
	// All three should fold into "PUSH1" via uppercase normalization.
	assert.Equal(t, map[string]int{"PUSH1": 7}, got)
}

func TestAggregateBlockOpcodes_RPCError(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"trace failed"}}`

	_, err := aggregateBlockOpcodes(resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trace failed")
}

func TestAggregateBlockOpcodes_EmptyResult(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"result":[]}`

	_, err := aggregateBlockOpcodes(resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no entries")
}

func TestAggregateBlockOpcodes_InvalidJSON(t *testing.T) {
	_, err := aggregateBlockOpcodes(`not json`)
	require.Error(t, err)
}
