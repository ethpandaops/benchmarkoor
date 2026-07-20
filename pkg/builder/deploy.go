package builder

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
)

// parseDeployerKey parses a 0x-optional hex private key and returns it plus its
// address, the account whose CREATE addresses the deployed contracts occupy.
func parseDeployerKey(hexKey string) (*ecdsa.PrivateKey, common.Address, error) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(hexKey, "0x"))
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("parsing deployer key: %w", err)
	}

	return key, crypto.PubkeyToAddress(key.PublicKey), nil
}

// runtimeBytecode decodes a 0x-optional runtime hex string to bytes.
func runtimeBytecode(code string) []byte {
	return common.FromHex(code)
}

// deriveSenderPool derives the first count addresses of an EEST distinct-sender
// pool: private key i is int(keccak256(seed)) + i, and the address is that key's
// EOA. This mirrors execution-specs' yield_distinct_sender (SENDER_BASE_KEY =
// int(keccak256(b"gas-repricings-private-key"))), so a pre-run can beacon-fund
// the pool that stateful ether-transfer benchmarks draw senders from.
func deriveSenderPool(seed string, count int) ([]common.Address, error) {
	base := new(big.Int).SetBytes(crypto.Keccak256([]byte(seed)))
	order := crypto.S256().Params().N

	addrs := make([]common.Address, 0, count)

	for i := range count {
		k := new(big.Int).Add(base, big.NewInt(int64(i)))
		if k.Sign() == 0 || k.Cmp(order) >= 0 {
			return nil, fmt.Errorf("derived sender key %d is out of the secp256k1 range", i)
		}

		kb := make([]byte, 32)
		k.FillBytes(kb)

		key, err := crypto.ToECDSA(kb)
		if err != nil {
			return nil, fmt.Errorf("deriving sender %d: %w", i, err)
		}

		addrs = append(addrs, crypto.PubkeyToAddress(key.PublicKey))
	}

	return addrs, nil
}

// maxDeployRuntimeSize is the largest runtime bytecode deployInitcode can wrap:
// its length is emitted as a PUSH2 operand.
const maxDeployRuntimeSize = 0xFFFF

// deployInitcode wraps runtime bytecode in the minimal init code that returns it
// verbatim: it copies the trailing runtime into memory (CODECOPY) and returns it
// (RETURN). This lets a plain CREATE transaction deploy arbitrary runtime
// bytecode (e.g. the EIP-8282 request contracts) to a CREATE-derived address, so
// the caller can point a fork's system-contract address params at it.
//
// The 14-byte prefix is:
//
//	PUSH2 <len> PUSH1 0x0e PUSH1 0x00 CODECOPY PUSH2 <len> PUSH1 0x00 RETURN
func deployInitcode(runtime []byte) ([]byte, error) {
	n := len(runtime)
	if n > maxDeployRuntimeSize {
		return nil, fmt.Errorf("runtime bytecode %d bytes exceeds max %d", n, maxDeployRuntimeSize)
	}

	hi, lo := byte(n>>8), byte(n)

	prefix := []byte{
		0x61, hi, lo, // PUSH2 len   (CODECOPY size)
		0x60, 0x0e, // PUSH1 0x0e  (runtime offset in the init code = prefix length)
		0x60, 0x00, // PUSH1 0x00  (memory dest)
		0x39,         // CODECOPY
		0x61, hi, lo, // PUSH2 len   (RETURN size)
		0x60, 0x00, // PUSH1 0x00  (RETURN offset)
		0xf3, // RETURN
	}

	return append(prefix, runtime...), nil
}

// signDeployTx builds and signs an EIP-1559 contract-creation transaction that
// deploys runtime via deployInitcode, and returns the raw (network-encoded)
// transaction plus the address the contract will occupy — crypto.CreateAddress
// of the signer and nonce, known before the tx is even sent.
func signDeployTx(
	key *ecdsa.PrivateKey,
	chainID *big.Int,
	nonce, gas uint64,
	gasFeeCap, gasTipCap *big.Int,
	runtime []byte,
) (rawTx []byte, addr common.Address, err error) {
	initcode, err := deployInitcode(runtime)
	if err != nil {
		return nil, common.Address{}, err
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gas,
		To:        nil, // contract creation
		Value:     big.NewInt(0),
		Data:      initcode,
	})

	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), key)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("signing deploy tx: %w", err)
	}

	rawTx, err = signed.MarshalBinary()
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("encoding deploy tx: %w", err)
	}

	from := crypto.PubkeyToAddress(key.PublicKey)

	return rawTx, crypto.CreateAddress(from, nonce), nil
}

// deployTipCap is the priority fee (1 gwei) deploy transactions pay.
var deployTipCap = big.NewInt(1_000_000_000)

// deployGas estimates a generous gas limit for deploying a runtime of the given
// length: intrinsic + init-code data cost (~16/byte) + init execution + CREATE +
// code deposit (200/byte), rounded up with headroom.
func deployGas(runtimeLen int) uint64 {
	return 200_000 + 350*uint64(runtimeLen)
}

// deployContracts deploys each runtime bytecode as its own contract-creation
// transaction from key (nonces 0..n-1) in a single block, and returns the
// CREATE-derived addresses. The deployer must already be funded (e.g. via the
// pre-run funding block). It prices the txs above the current base fee, builds
// one block carrying them (pre-fork, since forkAt selects the fork by
// timestamp), then verifies each address received code — failing loudly if a
// deploy reverted or the deployer was under-funded.
func (c *engineClient) deployContracts(
	ctx context.Context, key *ecdsa.PrivateKey, chainID *big.Int, runtimes [][]byte, log logrus.FieldLogger,
) ([]common.Address, error) {
	if len(runtimes) == 0 {
		return nil, nil
	}

	baseFee, err := c.latestBaseFee(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching base fee for deploy txs: %w", err)
	}

	// feeCap = 2*baseFee + tip covers a base-fee rise between build and inclusion.
	feeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), deployTipCap)
	if feeCap.Cmp(deployTipCap) < 0 {
		feeCap = new(big.Int).Set(deployTipCap)
	}

	from := crypto.PubkeyToAddress(key.PublicKey)

	rawTxs := make([][]byte, 0, len(runtimes))
	addrs := make([]common.Address, 0, len(runtimes))

	for i, runtime := range runtimes {
		raw, addr, signErr := signDeployTx(
			key, chainID, uint64(i), deployGas(len(runtime)), feeCap, deployTipCap, runtime,
		)
		if signErr != nil {
			return nil, fmt.Errorf("signing deploy tx %d: %w", i, signErr)
		}

		rawTxs = append(rawTxs, raw)
		addrs = append(addrs, addr)
	}

	log.WithFields(logrus.Fields{
		"deployer": from.Hex(), "contracts": len(runtimes), "addresses": addrs,
	}).Info("Deploying contracts before fork activation")

	if _, _, err := c.buildBlock(ctx, nil, rawTxs); err != nil {
		return nil, fmt.Errorf("building deploy block: %w", err)
	}

	for i, addr := range addrs {
		deployed, codeErr := c.code(ctx, addr.Hex())
		if codeErr != nil {
			return nil, fmt.Errorf("checking deployed code at %s: %w", addr.Hex(), codeErr)
		}

		if len(deployed) == 0 {
			return nil, fmt.Errorf(
				"deploy tx %d produced no code at %s (deployer %s under-funded or the tx reverted?)",
				i, addr.Hex(), from.Hex(),
			)
		}
	}

	log.WithField("addresses", addrs).Info("Contracts deployed")

	return addrs, nil
}
