package scenario

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// baseFee raises the base fee above the fee cap of some pool txs with full
// blocks, keeps it there, then lets it fall with empty blocks. A tx whose fee
// cap is below the base fee can never be in a block (hard). The pool should
// keep it while it is priced out, and include it once the base fee falls.
type baseFee struct{}

func (s *baseFee) Name() string { return "base-fee-movement" }

func (s *baseFee) Description() string {
	return "Full blocks raise the base fee above the fee cap of some txs; they must stay " +
		"out of every payload while priced out. Empty blocks then lower the base fee. " +
		"Checks that the pool keeps the priced-out txs and includes them in the first " +
		"block where they are valid."
}

func (s *baseFee) Defaults() Params {
	return Params{
		"capped_txs":        16,  // txs with a fee cap below the raised base fee
		"cap_percent":       150, // their fee cap, in percent of the start base fee
		"priced_out_blocks": 3,   // full blocks while they are priced out
		"max_blocks":        40,
		"timeout_ms":        30000,
	}
}

func (s *baseFee) Accounts(p Params) int { return p.Int("capped_txs") + 1 }

// fullBlock builds and imports one block of two gas-burning txs, which keeps
// the block nearly full, so the base fee rises by almost 12.5%.
func (s *baseFee) fullBlock(ctx context.Context, env *Env, burner *txfactory.Account,
) (*cldriver.Payload, error) {
	gas := env.Chain.GasLimit/2 - 50_000
	fees := txfactory.Fees{FeeCap: txfactory.GweiN(1000), TipCap: txfactory.GweiN(5)}

	burns := make([]*types.Transaction, 0, 2)

	for range 2 {
		tx, err := env.Factory.Build(burner, burn(fees, gas))
		if err != nil {
			return nil, err
		}

		burns = append(burns, tx)
	}

	if err := env.MustSend(ctx, burns); err != nil {
		return nil, err
	}

	head, err := env.Head(ctx)
	if err != nil {
		return nil, err
	}

	p, err := env.BuildWith(ctx, head, Hashes(burns), buildFull)
	if err != nil {
		return nil, err
	}

	if _, err := env.Import(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

func (s *baseFee) Run(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")

	accs, err := env.Accounts(ctx, p.Int("capped_txs")+1)
	if err != nil {
		return err
	}

	burner, capped := accs[0], accs[1:]

	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	startFee := head.NextBaseFee()
	feeCap := new(big.Int).Mul(startFee, big.NewInt(int64(p.Int("cap_percent"))))
	feeCap.Div(feeCap, big.NewInt(100))

	// 1. Raise the base fee above the cap.
	raise := 0

	for head.NextBaseFee().Cmp(feeCap) <= 0 {
		if raise >= p.Int("max_blocks") {
			return fmt.Errorf("the base fee did not pass %s after %d full blocks", feeCap, raise)
		}

		if _, err := s.fullBlock(ctx, env, burner); err != nil {
			return fmt.Errorf("raising the base fee: %w", err)
		}

		raise++

		if head, err = env.Head(ctx); err != nil {
			return err
		}
	}

	env.Rec.Event("basefee", "base fee %s -> %s after %d full blocks (cap %s)",
		startFee, head.NextBaseFee(), raise, feeCap)

	// 2. Send the capped txs: fee cap below the next base fee.
	txs := make([]*types.Transaction, 0, len(capped))

	for _, a := range capped {
		tx, err := env.Factory.Build(a, txfactory.Template{
			Kind: txfactory.KindDynamicFee,
			Fees: txfactory.Fees{FeeCap: feeCap, TipCap: txfactory.GweiN(1)},
		})
		if err != nil {
			return err
		}

		txs = append(txs, tx)
	}

	accepted := make([]*types.Transaction, 0, len(txs))
	var sendErr error

	for _, tx := range txs {
		if _, err := env.Send(ctx, tx); err != nil {
			sendErr = err

			continue
		}

		accepted = append(accepted, tx)
	}

	env.Rec.Observe("priced_out.admission", "Txs with a fee cap below the base fee: accepted of sent",
		fmt.Sprintf("%d of %d (%s)", len(accepted), len(txs), SendText(sendErr)))

	if len(accepted) == 0 {
		env.Rec.Check(SeveritySoft, false, "The pool accepts txs priced out by the base fee",
			"accepted", SendText(sendErr))

		return nil
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(accepted), timeout); err != nil {
		return fmt.Errorf("waiting for the capped txs: %w", err)
	}

	// 3. Keep the base fee high: they must stay out of every payload.
	includedEarly := 0

	for range p.Int("priced_out_blocks") {
		payload, err := s.fullBlock(ctx, env, burner)
		if err != nil {
			return err
		}

		for _, tx := range accepted {
			if payload.Contains(tx.Hash()) {
				includedEarly++
			}
		}
	}

	env.Rec.Check(SeverityHard, includedEarly == 0, "Txs priced out by the base fee are never in a payload",
		"0", fmt.Sprintf("%d", includedEarly))

	kept, err := countIn(ctx, env, accepted)
	if err != nil {
		return err
	}

	env.Rec.Check(SeveritySoft, kept == len(accepted), "The pool keeps txs while the base fee prices them out",
		fmt.Sprintf("%d kept", len(accepted)), fmt.Sprintf("%d kept", kept))

	if kept == 0 {
		return nil
	}

	// 4. Let the base fee fall with blocks that hold no burns.
	return s.fall(ctx, env, accepted, feeCap)
}

// fall imports blocks until the capped txs are in one. It records how many
// blocks that takes after the base fee drops below their cap.
func (s *baseFee) fall(ctx context.Context, env *Env, txs []*types.Transaction, feeCap *big.Int) error {
	pending := make(map[common.Hash]bool, len(txs))
	for _, tx := range txs {
		pending[tx.Hash()] = true
	}

	firstValid := -1 // index of the first block with base fee <= cap

	var validAt time.Time

	for i := range env.Params.Int("max_blocks") {
		head, err := env.Head(ctx)
		if err != nil {
			return err
		}

		payload, err := env.BuildOn(ctx, head, buildFull)
		if err != nil {
			return err
		}

		valid := payload.Block.BaseFee.Cmp(feeCap) <= 0
		if valid && firstValid < 0 {
			firstValid, validAt = i, time.Now()
		}

		in := 0

		for h := range pending {
			if payload.Contains(h) {
				in++

				delete(pending, h)
			}
		}

		if in > 0 && !valid {
			env.Rec.Check(SeverityHard, false, "Txs priced out by the base fee are never in a payload",
				"0", fmt.Sprintf("%d in block %d (base fee %s > cap %s)", in, payload.Block.Number,
					payload.Block.BaseFee, feeCap))
		}

		if _, err := env.Import(ctx, payload); err != nil {
			return err
		}

		if in > 0 {
			env.Rec.Event("basefee", "block %d: base fee %s (cap %s), %d capped txs included",
				payload.Block.Number, payload.Block.BaseFee, feeCap, in)
		}

		if len(pending) > 0 {
			continue
		}

		blocks := i - firstValid + 1

		env.Rec.Metric(Metric{
			Key: "blocks_to_include", Label: "Blocks from the first valid block until all are included",
			Unit: "blocks", Value: float64(blocks), Group: "fall", Better: "lower",
		})
		env.Rec.Metric(Metric{
			Key: "time_to_include", Label: "Time from the first valid build until all are included",
			Unit: "ms", Value: Ms(time.Since(validAt)), Group: "fall", Better: "lower",
		})
		env.Rec.Metric(Metric{
			Key: "fall_blocks", Label: "Blocks of falling base fee before the txs were valid",
			Unit: "blocks", Value: float64(firstValid), Group: "fall",
		})
		env.Rec.Check(SeveritySoft, blocks == 1, "Txs are included in the first block where they are valid",
			"block 1", fmt.Sprintf("block %d", blocks))

		return nil
	}

	env.Rec.Check(SeveritySoft, false, "Txs are included once the base fee falls below their cap",
		"all included", fmt.Sprintf("%d of %d not included", len(pending), len(txs)))

	return nil
}
