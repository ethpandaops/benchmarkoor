package builder

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeployInitcode(t *testing.T) {
	runtime := []byte{0x60, 0x00, 0x60, 0x00, 0xfd} // arbitrary 5-byte runtime

	initcode, err := deployInitcode(runtime)
	require.NoError(t, err)

	// 14-byte prefix + runtime.
	require.Len(t, initcode, 14+len(runtime))
	// Length operand (PUSH2) encodes the runtime size, and the copy offset is 0x0e.
	assert.Equal(t, byte(0x61), initcode[0])
	assert.Equal(t, byte(len(runtime)), initcode[2])
	assert.Equal(t, byte(0x0e), initcode[4])
	assert.Equal(t, byte(0xf3), initcode[13]) // RETURN closes the prefix
	// Runtime is appended verbatim after the prefix.
	assert.Equal(t, runtime, initcode[14:])
}

func TestDeployInitcode_TooLarge(t *testing.T) {
	_, err := deployInitcode(make([]byte, maxDeployRuntimeSize+1))
	require.ErrorContains(t, err, "exceeds max")
}

func TestSignDeployTx(t *testing.T) {
	key, err := crypto.HexToECDSA(
		"0000000000000000000000000000000000000000000000000000000000000001")
	require.NoError(t, err)

	from := crypto.PubkeyToAddress(key.PublicKey)
	chainID := big.NewInt(1)
	runtime := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}

	const nonce = 3

	rawTx, addr, err := signDeployTx(key, chainID, nonce, 500_000, big.NewInt(1e9), big.NewInt(1e9), runtime)
	require.NoError(t, err)

	// The deploy address is CREATE(from, nonce) — known before sending.
	assert.Equal(t, crypto.CreateAddress(from, nonce), addr)

	// The raw tx decodes to a valid, signed contract-creation whose sender and
	// nonce match, carrying the deploy init code.
	var tx types.Transaction
	require.NoError(t, tx.UnmarshalBinary(rawTx))
	assert.Nil(t, tx.To(), "contract creation")
	assert.Equal(t, uint64(nonce), tx.Nonce())

	wantInit, _ := deployInitcode(runtime)
	assert.Equal(t, wantInit, tx.Data())

	sender, err := types.Sender(types.LatestSignerForChainID(chainID), &tx)
	require.NoError(t, err)
	assert.Equal(t, from, sender)
	assert.NotEqual(t, common.Address{}, addr)
}

func TestParseDeployerKey(t *testing.T) {
	const hexKey = "0000000000000000000000000000000000000000000000000000000000000001"

	// With and without the 0x prefix, both parse to the same account.
	for _, in := range []string{hexKey, "0x" + hexKey} {
		key, addr, err := parseDeployerKey(in)
		require.NoError(t, err)
		assert.Equal(t, crypto.PubkeyToAddress(key.PublicKey), addr)
		// Private key 0x..01 → the well-known address.
		assert.Equal(t,
			common.HexToAddress("0x7e5f4552091a69125d5dfcb7b8c2659029395bdf"), addr)
	}

	_, _, err := parseDeployerKey("0xnothex")
	require.Error(t, err)
}

func TestRuntimeBytecode(t *testing.T) {
	assert.Equal(t, []byte{0x60, 0x00, 0xfd}, runtimeBytecode("0x6000fd"))
	assert.Equal(t, []byte{0x60, 0x00, 0xfd}, runtimeBytecode("6000fd"))
	assert.Empty(t, runtimeBytecode("0x"))
}

func TestDeriveSenderPool(t *testing.T) {
	addrs, err := deriveSenderPool("gas-repricings-private-key", 4)
	require.NoError(t, err)
	require.Len(t, addrs, 4)

	// Deterministic: sender[0] matches execution-specs' SENDER_BASE_KEY EOA
	// (the address that fails "insufficient funds ... 0x4e5e..." unfunded).
	assert.Equal(t, common.HexToAddress("0x4e5e4CBB5d1c13242118aA32f02c7723D9c9377a"), addrs[0])

	// Distinct + stable across calls.
	again, err := deriveSenderPool("gas-repricings-private-key", 4)
	require.NoError(t, err)
	assert.Equal(t, addrs, again)
	assert.NotEqual(t, addrs[0], addrs[1])
}

func TestExpandFundingAccounts(t *testing.T) {
	amt := uint64(5)
	tgt := &config.PreRunTarget{
		FundingAccounts: []config.PreRunFundingAccount{{Address: "0xseed"}},
		FundingPools:    []config.PreRunFundingPool{{BaseKeySeed: "gas-repricings-private-key", Count: 3, AmountGwei: &amt}},
	}

	accounts, err := expandFundingAccounts(tgt)
	require.NoError(t, err)
	require.Len(t, accounts, 1+3, "explicit account + 3 pool addresses")
	assert.Equal(t, "0xseed", accounts[0].Address)
	assert.Equal(t, "0x4e5e4CBB5d1c13242118aA32f02c7723D9c9377a", accounts[1].Address)
	require.NotNil(t, accounts[1].AmountGwei)
	assert.Equal(t, uint64(5), *accounts[1].AmountGwei)
}

func TestDeployGas(t *testing.T) {
	// Monotonic in runtime length, with a fixed floor for the empty runtime.
	assert.Equal(t, uint64(200_000), deployGas(0))
	assert.Greater(t, deployGas(1000), deployGas(0))
}
