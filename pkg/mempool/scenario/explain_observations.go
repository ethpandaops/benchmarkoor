package scenario

import "strings"

// Observations have no expected value, so the third text of their
// Explanation (OK) says how to read the value: which answers are common and
// what each one means.

var observationExact = map[string]*Explanation{
	"reject.duplicate": ex(
		"The answer to sending a tx that is already in the pool.",
		"Wallets resend txs; the answer tells them whether the pool has it.",
		"\"already known\" (or the tx hash) are both fine. The pool size must not change (a hard check)."),

	"overdraft.accepted": ex(
		"How many of 6 txs the pool accepted from an account that can pay for 3.",
		"It shows whether the pool counts the cost of all of a sender's pending txs, or checks each tx alone.",
		"3: the pool counts the total (strict). 6: it checks each tx alone; the extra 3 can never all run."),
	"overdraft.error": ex(
		"The error for the first tx the account's balance cannot pay.",
		"A clear reason helps users find out why their tx was refused.",
		"Any \"insufficient funds\" text is fine. Absent when the pool accepted all 6."),
	"far_nonce": ex(
		"The answer to a tx with a nonce 1000 above the account nonce.",
		"Far-future nonces can never run soon; keeping many is a cheap way to fill a pool.",
		"\"accepted\" (queued) or a \"nonce too far\" rejection are both fine pool policies."),
	"cumulative.second": ex(
		"The answer to a 5 ETH tx from a 10 ETH account that already has an 8 ETH tx pending.",
		"Each tx alone is payable; together they are not.",
		"A rejection (insufficient funds) means the pool counts value across txs. \"accepted\" means it does not; "+
			"the tx must then never be included (a hard check)."),

	"order.actual": ex(
		"The first 12 txs of the built payload: effective tip in gwei and sender:nonce (s0 = the first sender "+
			"seen).",
		"It shows the builder's ordering policy.",
		"Compare with order.expected. Same order: greedy by tip. Grouped by sender in arrival order: besu's "+
			"policy. Nonce order within a sender must always hold (a hard check)."),
	"order.expected": ex(
		"The same 12 txs in the greedy order: at each step the sender whose next tx pays the highest tip.",
		"The reference for order.actual.",
		"A reference, not a result."),

	"evict.size_after": ex(
		"Pool size (pending+queued) after the higher-tip txs arrive in the full pool.",
		"It shows what the client counts as its limit.",
		"About global_slots + global_queue (6144) for count-based pools; more for pools that limit by memory "+
			"(besu)."),
	"evict.cheap": ex(
		"What happened to 40 txs cheaper than everything in the full pool: rejected, or kept.",
		"Keeping them means the pool evicted better txs or was not full.",
		"All rejected (\"underpriced\" or \"pool full\") is the common answer for a full pool."),
	"evict.never_full": ex(
		"Set when the pool took every tx and dropped none, so it never was full.",
		"The full-pool checks need a full pool; they are skipped then.",
		"Common for pools that limit by memory and not by tx count."),
	"hog.kept": ex(
		"How many of 256 txs from one sender the pool kept, and the error for the rest.",
		"A per-sender cap stops one account from taking the pool.",
		"16 (a hard per-sender cap, as reth), 64 (besu's nonce distance) or all 256 (a cap that applies only "+
			"when the pool is full) are all seen. Policy."),
	"queue.kept": ex(
		"How many of 80 future-nonce txs from one sender the pool kept (account queue 64).",
		"It shows the per-account queue limit.",
		"64 matches the pinned limit. Clients without a per-account queue limit keep all 80."),
	"gapped.kept": ex(
		"How many of 1024 gapped txs (64 senders) the pool kept, and the pool size after.",
		"Gapped txs can never run until the gap fills, so they cost the attacker nothing.",
		"Up to global_queue (1024) is the pinned limit; fewer is fine."),
	"big.kept": ex(
		"How many of 200 txs with 120 KiB of calldata the pool kept, and the pool size before and after.",
		"Large txs use much more memory per tx; pools that limit by memory behave differently from pools that "+
			"count txs.",
		"All kept is common at this size (about 23 MiB). Rejections show a memory or slot limit."),

	"priced_out.admission": ex(
		"How many txs with a fee cap below the next base fee the pool accepted.",
		"The base fee can fall again, so such txs may become valid later.",
		"All accepted is common. Rejected is a stricter policy."),
	"zero_tip.admission": ex(
		"The answer to a tx whose fee cap equals the base fee, so it pays no tip.",
		"The tx is valid but pays the builder nothing.",
		"\"accepted\" is common."),
	"zero_tip.included": ex(
		"Whether the next payload holds the zero-tip tx.",
		"Builders may require a minimum tip (geth's miner.gasprice).",
		"true or false are both fine builder policies."),

	"blob_fee.admission": ex(
		"The answer to a blob tx whose max fee per blob gas is below the current blob base fee.",
		"The blob base fee can fall again, but it rarely does fast.",
		"\"accepted\" (kept until it is valid) or an \"underpriced\" rejection are both fine pool policies."),
	"blob_fee.kept": ex(
		"Whether that priced-out blob tx is still in the pool after 3 full-blob blocks.",
		"Blob txs are large; keeping priced-out ones costs memory.",
		"true or false are both fine. It must never be included while priced out (a hard check)."),
	"per_sender": ex(
		"How many of 24 blob txs from one sender the pool accepted and kept.",
		"Blob pools cap blob txs per account to bound memory.",
		"16 (geth's per-account limit) is common; others use their own caps or none."),

	"drain.pending": ex(
		"How many of 4 txs the account delegated to the sweeper could have pending.",
		"Pools limit pending txs of delegated accounts, since their code can move funds at any time.",
		"1 is common (an in-flight limit for delegated accounts)."),

	"inflight.delegated": ex(
		"How many of 5 txs a delegated account could have pending.",
		"A delegated account's balance can change without a tx from it, so pools limit what they trust.",
		"1 is common. More means the pool trusts delegated accounts like plain ones."),
	"nonce_bump.admission": ex(
		"The answer to a set-code tx whose authorization uses the nonce of an account with a pending tx.",
		"Both strategies keep the chain correct: refuse the set-code tx, or accept it and drop the "+
			"invalidated tx after the block.",
		"\"accepted\" (geth, besu, reth, erigon) or a \"pending transaction\" rejection (nethermind) are "+
			"both fine."),
	"revoke.delegated": ex(
		"How many of 3 txs the account could have pending while delegated.",
		"The baseline for revoke.revoked.",
		"1 is common."),
	"revoke.revoked": ex(
		"How many of 3 txs the account could have pending after it revoked its delegation.",
		"After a revoke, the account is a plain account again.",
		"3 is expected."),
	"bad_auths.admission": ex(
		"The answer to a set-code tx with three invalid authorizations and one valid one.",
		"Invalid authorizations are skipped at execution; they do not make the tx invalid.",
		"\"accepted\" is common. A rejection is a stricter policy."),
}

// observationPrefixes explain observations whose key ends in a case.
var observationPrefixes = []struct {
	prefix string
	exp    func(rest string) *Explanation
}{
	{"reject.", func(rest string) *Explanation {
		return ex("The answer of eth_sendRawTransaction to this invalid tx: the error text, or \"accepted\".",
			"The error text is what users and tools see. A clear, specific reason helps them fix the tx; a "+
				"generic one (\"Invalid params\", \"Internal error\") does not.",
			"Any clear rejection is fine; the wording differs between clients. Whether it must be rejected is "+
				"the paired \"Rejects:\" check.")
	}},
	{"replace.", func(rest string) *Explanation {
		if rest == "mid_chain" {
			return ex("The answer to replacing nonce 1 of a 4-tx chain with a tx that leaves nothing for nonces 2 "+
				"and 3.",
				"A pool that counts the whole chain's cost refuses it; others accept it and must then deal with "+
					"the unpayable later txs.",
				"A rejection (geth, erigon) or \"accepted\" are both fine. If accepted, nonces 2 and 3 must "+
					"never be included (a hard check).")
		}

		return ex("The answer to this replacement: the error text, or \"accepted\".",
			"It shows the client's replacement rule and how it explains a refusal.",
			"\"replacement transaction underpriced\" or similar for a refusal. Whether it should be accepted "+
				"is the paired \"Replacement:\" check.")
	}},
	{"round", func(string) *Explanation {
		return ex("How many of 40 txs sent through a 4 s build window are in the payload, and when the missed "+
			"ones were sent (ms after the build started).",
			"It shows how often the builder adds new txs to a payload while it builds.",
			"Missed txs sent after some offset mean the builder stopped adding txs then. Geth rebuilds every "+
				"2 s, so it misses txs after about 2000 ms.")
	}},
}

// ExplainObservation returns the explanation of an observation key, or nil.
func ExplainObservation(key string) *Explanation {
	if e, ok := observationExact[key]; ok {
		return e
	}

	for _, p := range observationPrefixes {
		if rest, ok := strings.CutPrefix(key, p.prefix); ok {
			return p.exp(rest)
		}
	}

	return nil
}
