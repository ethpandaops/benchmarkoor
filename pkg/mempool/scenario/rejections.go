package scenario

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// rejections sends invalid txs and checks that the pool rejects them. The
// error text of each rejection is an observation, so the UI can compare it
// between clients.
type rejections struct{}

func (s *rejections) Name() string { return "admission-rejections" }

func (s *rejections) Description() string {
	return "Sends invalid txs (nonce too low, intrinsic gas, EIP-7825 gas cap, wrong chain id, " +
		"tip above fee cap, oversize, initcode too large, no funds, underpriced, blob sidecar " +
		"version 0 after Osaka, blob tx without blobs, set-code tx without authorizations) and " +
		"checks that the pool rejects each one."
}

func (s *rejections) Defaults() Params {
	return Params{"timeout_ms": 10000}
}

// severityObserve records only the observation, with no check.
const severityObserve Severity = ""

// rejectionCase is one invalid tx.
type rejectionCase struct {
	key   string
	label string
	// sev is hard when accepting the tx breaks a protocol rule, soft when
	// the rule is a pool policy.
	sev   Severity
	build func(env *Env, a *txfactory.Account) (*types.Transaction, error)
}

func (s *rejections) cases() []rejectionCase {
	nonce0 := func() *uint64 { return new(uint64(0)) }
	fees := txfactory.DefaultFees()
	dyn := func(t txfactory.Template) txfactory.Template {
		t.Kind = txfactory.KindDynamicFee
		if t.Fees.FeeCap == nil {
			t.Fees = fees
		}

		return t
	}

	return []rejectionCase{
		{"nonce_too_low", "Nonce below the account nonce", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{Nonce: nonce0()}))
			}},
		{"intrinsic_gas", "Gas limit below the intrinsic gas", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{Gas: 20_999}))
			}},
		{"gas_above_tx_cap", "Gas limit above the EIP-7825 cap", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{Gas: env.Chain.MaxTxGas + 1}))
			}},
		{"gas_above_block_limit", "Gas limit above the block gas limit", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{Gas: env.Chain.GasLimit + 1}))
			}},
		{"wrong_chain_id", "Wrong chain id", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				id := new(big.Int).Add(env.Factory.ChainID(), big.NewInt(1))

				return env.Factory.Build(a, dyn(txfactory.Template{ChainID: id}))
			}},
		{"tip_above_fee_cap", "maxPriorityFeePerGas above maxFeePerGas", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{
					Fees: txfactory.Fees{FeeCap: txfactory.GweiN(1), TipCap: txfactory.GweiN(2)},
				}))
			}},
		// The balance can rise later, so keeping the tx is pool policy.
		{"no_funds", "Value above the sender balance", SeveritySoft,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				huge := new(big.Int).Lsh(big.NewInt(1), 200)

				return env.Factory.Build(a, dyn(txfactory.Template{Value: huge}))
			}},
		{"initcode_too_large", "Contract creation with initcode above 49152 bytes", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{
					Data: make([]byte, 49_153), Gas: 3_000_000, Value: big.NewInt(0),
				}))
			}},
		// The sidecar is not part of the block, so a client may convert a
		// version 0 sidecar instead of rejecting the tx: a pool policy.
		{"blob_v0_sidecar", "Blob tx with a version 0 sidecar (no cell proofs) after Osaka", SeveritySoft,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, txfactory.Template{
					Kind: txfactory.KindBlob, SidecarVersion: new(types.BlobSidecarVersion0),
				})
			}},
		{"blob_no_blobs", "Blob tx with no blobs", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return blobless(env, a)
			}},
		{"setcode_no_auth", "Set-code tx with an empty authorization list", SeverityHard,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, txfactory.Template{Kind: txfactory.KindSetCode, NoAuth: true})
			}},
		{"oversize", "Tx above 128 KiB", SeveritySoft,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{
					Data: make([]byte, 132*1024), Gas: env.Chain.MaxTxGas - 1,
				}))
			}},
		{"underpriced", "Gas price below the pool minimum (0 wei)", SeveritySoft,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, txfactory.Template{
					Kind: txfactory.KindLegacy,
					Fees: txfactory.Fees{FeeCap: big.NewInt(0), TipCap: big.NewInt(0)},
				})
			}},
		// No correct answer: a pool may keep the tx until the base fee drops.
		{"fee_cap_below_base_fee", "maxFeePerGas below the base fee", severityObserve,
			func(env *Env, a *txfactory.Account) (*types.Transaction, error) {
				return env.Factory.Build(a, dyn(txfactory.Template{
					Fees: txfactory.Fees{FeeCap: big.NewInt(1), TipCap: big.NewInt(1)},
				}))
			}},
	}
}

// blobless returns a blob tx with no blob hashes and no sidecar.
func blobless(env *Env, a *txfactory.Account) (*types.Transaction, error) {
	fees := txfactory.DefaultFees()
	to := a.Address

	return env.Factory.Sign(a, &types.BlobTx{
		ChainID:    u256Of(env.Factory.ChainID()),
		Nonce:      a.Nonce(),
		GasTipCap:  u256Of(fees.TipCap),
		GasFeeCap:  u256Of(fees.FeeCap),
		Gas:        21_000,
		To:         to,
		BlobFeeCap: u256Of(fees.BlobFeeCap),
		Sidecar:    types.NewBlobTxSidecar(types.BlobSidecarVersion1, nil, nil, nil),
	})
}

func (s *rejections) Accounts(Params) int { return 2 + len(s.cases()) }

func (s *rejections) Run(ctx context.Context, env *Env) error {
	timeout := env.Params.Duration("timeout_ms")

	// "Nonce too low" needs one mined tx first.
	accounts, err := env.Accounts(ctx, 2+len(s.cases()))
	if err != nil {
		return err
	}

	control, nonceSender, rest := accounts[0], accounts[1], accounts[2:]

	first, err := env.Factory.Transfer(nonceSender, txfactory.KindDynamicFee, txfactory.DefaultFees())
	if err != nil {
		return err
	}

	if err := env.MustSend(ctx, []*types.Transaction{first}, "one tx to mine, for nonce too low"); err != nil {
		return err
	}

	if _, err := env.Drain(ctx, 4); err != nil {
		return err
	}

	// A valid tx first: if this fails, every rejection below means nothing.
	valid, err := env.Factory.Transfer(control, txfactory.KindDynamicFee, txfactory.DefaultFees())
	if err != nil {
		return err
	}

	_, err = env.Send(ctx, valid)
	if !env.Rec.Check(SeverityHard, err == nil, "A valid control tx is accepted", "accepted", SendText(err)) {
		return fmt.Errorf("the control tx was rejected: %w", err)
	}

	mustReject := make(map[common.Hash]string, len(s.cases()))

	env.Rec.Event("send", "%d invalid txs, one per case", len(s.cases()))

	for i, c := range s.cases() {
		sender := rest[i]
		if c.key == "nonce_too_low" {
			sender = nonceSender
		}

		tx, err := c.build(env, sender)
		if err != nil {
			return fmt.Errorf("%s: %w", c.key, err)
		}

		took, sendErr := env.Send(ctx, tx)

		// Only a rejection is timed as one. An accepted tx did other work
		// (geth converts a v0 blob sidecar to cell proofs), which has its
		// own metric.
		switch {
		case sendErr != nil:
			env.Rec.Latency("reject_latency", "Time to reject an invalid tx", "rejection", took)
		case c.key == "blob_v0_sidecar":
			env.Rec.Metric(Metric{
				Key: "v0_sidecar_accept", Label: "Time to accept (and convert) a v0 blob sidecar",
				Unit: "ms", Value: Ms(took), Group: "rejection", Better: "lower",
			})
		}
		env.Rec.Observe("reject."+c.key, c.label, SendText(sendErr))
		if c.sev != severityObserve {
			env.Rec.Check(c.sev, sendErr != nil, "Rejects: "+c.label, "rejected", SendText(sendErr))
		}

		if c.sev == SeverityHard {
			mustReject[tx.Hash()] = c.key
		}
	}

	// A tx that breaks a protocol rule must never reach a payload, even if
	// the pool accepted it.
	payload, err := env.Build(ctx, buildFull)
	if err != nil {
		return err
	}

	invalid := make([]string, 0, len(payload.Txs))

	for _, h := range payload.TxHashes {
		if key, bad := mustReject[h]; bad {
			invalid = append(invalid, key)
		}
	}

	env.Rec.Check(SeverityHard, len(invalid) == 0, "No invalid tx is in the built payload",
		"none", fmt.Sprintf("%v", invalid))
	env.Rec.Check(SeverityHard, payload.Contains(valid.Hash()),
		"The valid control tx is in the built payload", "included", fmt.Sprintf("%d txs", len(payload.Txs)))

	_, err = env.WaitInPool(ctx, time.Now(), Hashes([]*types.Transaction{valid}), timeout)

	return err
}
