package scenario

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// setcode tests the pool rules around EIP-7702 delegations. A delegated
// account runs code that can move its funds at any time, and an
// authorization in someone else's tx changes the authority's nonce, so the
// pool can no longer trust what it knew about such an account.
type setcode struct{}

func (s *setcode) Name() string { return "setcode-delegation" }

func (s *setcode) Description() string {
	return "EIP-7702 pool rules: how many pending txs a delegated account may have; an " +
		"authorization in another sender's tx that moves an account's nonce (its pending tx " +
		"becomes invalid); revoking a delegation; authorizations with a wrong chain id, a bad " +
		"signature or a stale nonce (the tx stays valid, the authorization is skipped)."
}

func (s *setcode) Defaults() Params {
	return Params{
		"inflight_txs": 5, // txs a delegated account tries to have pending
		"timeout_ms":   20000,
	}
}

func (s *setcode) Accounts(Params) int { return 12 }

// delegationTarget is a delegation target with no code: delegating to it
// makes an account "delegated" for the pool without giving it behaviour.
var delegationTarget = common.HexToAddress("0x000000000000000000000000000000000000dEaD")

func (s *setcode) Run(ctx context.Context, env *Env) error {
	accs, err := env.Accounts(ctx, 12)
	if err != nil {
		return err
	}

	sponsor := accs[0]

	if err := s.inflight(ctx, env, sponsor, accs[1]); err != nil {
		return fmt.Errorf("in-flight: %w", err)
	}

	if err := s.nonceBump(ctx, env, sponsor, accs[2]); err != nil {
		return fmt.Errorf("nonce bump: %w", err)
	}

	if err := s.revoke(ctx, env, sponsor, accs[3]); err != nil {
		return fmt.Errorf("revoke: %w", err)
	}

	if err := s.badAuths(ctx, env, sponsor, accs[4:8]); err != nil {
		return fmt.Errorf("bad authorizations: %w", err)
	}

	_, err = env.Drain(ctx, 8)

	return err
}

// delegate makes authority delegate to target: the sponsor sends a set-code
// tx with the authority's authorization, and the next block includes it.
func delegate(ctx context.Context, env *Env, sponsor, authority *txfactory.Account, target common.Address,
) error {
	if err := env.syncNonces(ctx, []*txfactory.Account{authority}); err != nil {
		return err
	}

	auth, err := env.Factory.SignAuth(authority, nil, target, authority.Nonce())
	if err != nil {
		return err
	}

	tx, err := env.Factory.Build(sponsor, txfactory.Template{
		Kind: txfactory.KindSetCode, AuthList: []types.SetCodeAuthorization{auth},
	})
	if err != nil {
		return err
	}

	if err := env.MustSend(ctx, []*types.Transaction{tx}, "delegate "+short(authority.Address)); err != nil {
		return err
	}

	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	p, err := env.BuildWith(ctx, head, []common.Hash{tx.Hash()}, buildFull)
	if err != nil {
		return err
	}

	if _, err := env.Import(ctx, p); err != nil {
		return err
	}

	authority.SetNonce(authority.Nonce() + 1)

	got, err := env.DelegatedTo(ctx, authority.Address)
	if err != nil {
		return err
	}

	if got != target {
		return fmt.Errorf("%s delegates to %s after the set-code tx, want %s", short(authority.Address), got, target)
	}

	return nil
}

func short(a common.Address) string { return a.Hex()[:10] }

// pricedOut returns fees just below the base fee of the next block: the tx
// stays in the pool now, and becomes valid by price once empty blocks lower
// the base fee.
func pricedOut(head *cldriver.Block) txfactory.Fees {
	feeCap := new(big.Int).Mul(head.NextBaseFee(), big.NewInt(9))
	feeCap.Div(feeCap, big.NewInt(10))

	return txfactory.Fees{FeeCap: feeCap, TipCap: new(big.Int).Div(feeCap, big.NewInt(2))}
}

// inflight: a delegated account tries to have several pending txs.
func (s *setcode) inflight(ctx context.Context, env *Env, sponsor, x *txfactory.Account) error {
	if err := delegate(ctx, env, sponsor, x, delegationTarget); err != nil {
		return err
	}

	n := env.Params.Int("inflight_txs")
	accepted := 0

	var firstErr error

	env.Rec.Event("send", "delegated account: %d txs", n)

	for range n {
		tx, err := env.Factory.Transfer(x, txfactory.KindDynamicFee, txfactory.DefaultFees())
		if err != nil {
			return err
		}

		if _, err := env.Send(ctx, tx); err != nil {
			x.SetNonce(tx.Nonce())

			if firstErr == nil {
				firstErr = err
			}

			continue
		}

		accepted++
	}

	env.Rec.Metric(Metric{
		Key: "inflight.delegated", Label: "Pending txs a delegated account may have", Unit: "txs",
		Value: float64(accepted), Group: "in-flight",
	})
	env.Rec.Observe("inflight.delegated", fmt.Sprintf("Delegated account sends %d txs: accepted", n),
		fmt.Sprintf("%d (%s)", accepted, SendText(firstErr)))

	_, err := env.Drain(ctx, 4)

	return err
}

// nonceBump: Y has a pending tx at nonce n. The sponsor's set-code tx carries
// Y's authorization at nonce n, which moves Y's nonce to n+1 when it runs.
// Y's pending tx is then invalid: it must never be in a payload, and the pool
// should drop it.
func (s *setcode) nonceBump(ctx context.Context, env *Env, sponsor, y *txfactory.Account) error {
	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	n := y.Nonce()

	yTx, err := env.Factory.Build(y, txfactory.Template{Kind: txfactory.KindDynamicFee, Fees: pricedOut(head)})
	if err != nil {
		return err
	}

	if err := env.MustSend(ctx, []*types.Transaction{yTx}, "Y: pending tx at its nonce, priced out"); err != nil {
		return err
	}

	auth, err := env.Factory.SignAuth(y, nil, delegationTarget, n)
	if err != nil {
		return err
	}

	sTx, err := env.Factory.Build(sponsor, txfactory.Template{
		Kind: txfactory.KindSetCode, AuthList: []types.SetCodeAuthorization{auth},
	})
	if err != nil {
		return err
	}

	_, sendErr := env.Send(ctx, sTx)
	env.Rec.Event("send", "sponsor: set-code tx with Y's authorization at Y's pending nonce")
	env.Rec.Observe("nonce_bump.admission",
		"Set-code tx carrying the authorization of an account with a pending tx", SendText(sendErr))

	if sendErr != nil {
		sponsor.SetNonce(sTx.Nonce())

		// Some pools refuse the set-code tx instead; then Y's tx stays valid.
		return env.cleanPricedOut(ctx, []*types.Transaction{yTx})
	}

	p, err := env.BuildWith(ctx, head, []common.Hash{sTx.Hash()}, buildFull)
	if err != nil {
		return err
	}

	env.Rec.Check(SeverityHard, !p.Contains(yTx.Hash()), "A priced-out tx is not in the payload", "not included",
		fmt.Sprintf("included: %v", p.Contains(yTx.Hash())))

	start := time.Now()

	if _, err := env.Import(ctx, p); err != nil {
		return err
	}

	var nonce hexutil.Uint64
	if err := env.Pool.RPC().CallContext(ctx, &nonce, "eth_getTransactionCount", y.Address, "latest"); err != nil {
		return err
	}

	env.Rec.Check(SeverityHard, uint64(nonce) == n+1, "A valid authorization moves the authority's nonce",
		fmt.Sprintf("nonce %d", n+1), fmt.Sprintf("nonce %d", nonce))

	gone, goneErr := env.WaitGone(ctx, start, []common.Hash{yTx.Hash()}, 5*time.Second)
	env.Rec.Check(SeveritySoft, goneErr == nil, "The pool drops a tx whose nonce an authorization used",
		"dropped", ErrText(goneErr))

	if goneErr == nil {
		env.Rec.Metric(Metric{
			Key: "nonce_bump.dropped", Label: "Time until the pool drops the invalidated tx", Unit: "ms",
			Value: Ms(gone), Group: "nonce bump", Better: "lower",
		})
	}

	// Lower the base fee until Y's tx would be valid by price. It must still
	// never be in a payload: its nonce is used.
	included := 0

	for range 4 {
		p, err := env.ImportNext(ctx, buildFull)
		if err != nil {
			return err
		}

		if p.Contains(yTx.Hash()) {
			included++
		}
	}

	env.Rec.Check(SeverityHard, included == 0, "A tx invalidated by an authorization is never in a payload",
		"0", fmt.Sprintf("%d", included))

	y.SetNonce(n + 1)

	return nil
}

// cleanPricedOut makes priced-out txs valid by price and includes them, so
// the scenario goes on from a clean pool.
func (e *Env) cleanPricedOut(ctx context.Context, txs []*types.Transaction) error {
	for range 6 {
		in, err := e.Pool.InPool(ctx, Hashes(txs))
		if err != nil {
			return err
		}

		left := 0
		for _, tx := range txs {
			if in[tx.Hash()] {
				left++
			}
		}

		if left == 0 {
			return nil
		}

		if _, err := e.ImportNext(ctx, buildFull); err != nil {
			return err
		}
	}

	return nil
}

// revoke: a delegated account revokes its delegation (delegates to the zero
// address). After that, the pool should treat it as a plain account again.
func (s *setcode) revoke(ctx context.Context, env *Env, sponsor, w *txfactory.Account) error {
	if err := delegate(ctx, env, sponsor, w, delegationTarget); err != nil {
		return err
	}

	send := func(label string) (int, error) {
		accepted := 0

		var firstErr error

		env.Rec.Event("send", "%s: 3 txs", label)

		for range 3 {
			tx, err := env.Factory.Transfer(w, txfactory.KindDynamicFee, txfactory.DefaultFees())
			if err != nil {
				return 0, err
			}

			if _, err := env.Send(ctx, tx); err != nil {
				w.SetNonce(tx.Nonce())

				if firstErr == nil {
					firstErr = err
				}

				continue
			}

			accepted++
		}

		env.Rec.Observe("revoke."+label, fmt.Sprintf("Txs accepted of 3 (%s)", label),
			fmt.Sprintf("%d (%s)", accepted, SendText(firstErr)))

		return accepted, nil
	}

	before, err := send("delegated")
	if err != nil {
		return err
	}

	if _, err := env.Drain(ctx, 4); err != nil {
		return err
	}

	if err := env.syncNonces(ctx, []*txfactory.Account{w}); err != nil {
		return err
	}

	if err := delegate(ctx, env, sponsor, w, common.Address{}); err != nil {
		return fmt.Errorf("revoking: %w", err)
	}

	code, err := env.Code(ctx, w.Address)
	if err != nil {
		return err
	}

	env.Rec.Check(SeverityHard, len(code) == 0, "Delegating to the zero address clears the code",
		"no code", fmt.Sprintf("%d bytes", len(code)))

	after, err := send("revoked")
	if err != nil {
		return err
	}

	env.Rec.Check(SeveritySoft, after >= before && after == 3,
		"After revoking, the account may have pending txs like a plain account",
		"3 accepted", fmt.Sprintf("%d accepted (was %d while delegated)", after, before))

	_, err = env.Drain(ctx, 4)

	return err
}

// badAuths: one set-code tx with four authorizations. Three are invalid
// (wrong chain id, bad signature, stale nonce) and must be skipped; the
// fourth (chain id 0, valid on any chain) must apply.
func (s *setcode) badAuths(ctx context.Context, env *Env, sponsor *txfactory.Account, auths []*txfactory.Account,
) error {
	if err := env.syncNonces(ctx, auths); err != nil {
		return err
	}

	wrongChain, err := env.Factory.SignAuth(auths[0], new(big.Int).Add(env.Factory.ChainID(), big.NewInt(1)),
		delegationTarget, auths[0].Nonce())
	if err != nil {
		return err
	}

	badSig, err := env.Factory.SignAuth(auths[1], nil, delegationTarget, auths[1].Nonce())
	if err != nil {
		return err
	}

	badSig.R = *uint256.NewInt(0)

	stale, err := env.Factory.SignAuth(auths[2], nil, delegationTarget, auths[2].Nonce()+5)
	if err != nil {
		return err
	}

	anyChain, err := env.Factory.SignAuth(auths[3], big.NewInt(0), delegationTarget, auths[3].Nonce())
	if err != nil {
		return err
	}

	tx, err := env.Factory.Build(sponsor, txfactory.Template{
		Kind:     txfactory.KindSetCode,
		AuthList: []types.SetCodeAuthorization{wrongChain, badSig, stale, anyChain},
	})
	if err != nil {
		return err
	}

	_, sendErr := env.Send(ctx, tx)
	env.Rec.Event("send", "set-code tx with 3 invalid authorizations and 1 valid one")
	env.Rec.Observe("bad_auths.admission", "Set-code tx with invalid authorizations (the tx itself is valid)",
		SendText(sendErr))
	env.Rec.Check(SeveritySoft, sendErr == nil, "A set-code tx with some invalid authorizations is accepted",
		"accepted", SendText(sendErr))

	if sendErr != nil {
		sponsor.SetNonce(tx.Nonce())

		return nil
	}

	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	p, err := env.BuildWith(ctx, head, []common.Hash{tx.Hash()}, buildFull)
	if err != nil {
		return err
	}

	_, err = env.Import(ctx, p)
	env.Rec.Check(SeverityHard, err == nil, "The block with invalid authorizations is valid", "VALID", ErrText(err))

	if err != nil {
		return err
	}

	for i, c := range []struct {
		name string
		want common.Address
	}{
		{"wrong chain id", common.Address{}},
		{"bad signature", common.Address{}},
		{"stale nonce", common.Address{}},
		{"chain id 0 (any chain)", delegationTarget},
	} {
		got, err := env.DelegatedTo(ctx, auths[i].Address)
		if err != nil {
			return err
		}

		want := "not delegated"
		if c.want != (common.Address{}) {
			want = "delegated"
		}

		env.Rec.Check(SeverityHard, got == c.want, "Authorization with "+c.name+": "+want, want,
			fmt.Sprintf("delegates to %s", got))
	}

	return nil
}
