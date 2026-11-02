package scenario

import "strings"

// Explanation says what a check tests, why it matters and which results are
// fine. The recorder attaches it to every check by name.
type Explanation struct {
	What string `json:"what"`
	Why  string `json:"why"`
	OK   string `json:"ok"`
}

func ex(what, why, ok string) *Explanation { return &Explanation{What: what, Why: why, OK: ok} }

// Shared texts for checks that many cases use.
var (
	exNeverIncluded = func(what string) *Explanation {
		return ex(what,
			"A block that includes it would be invalid (or would run a tx twice). A builder must never pick it, "+
				"whatever its pool keeps.",
			"Only \"not included\". Any inclusion is a builder bug.")
	}
	exPolicyReject = func(what, note string) *Explanation {
		return ex(what,
			"Pool policy, not a protocol rule: the tx is not invalid forever. "+note,
			"Rejected is the expected default. Accepted is a finding, not a bug, if the tx never reaches an "+
				"invalid payload.")
	}
	exProtocolReject = func(what, rule string) *Explanation {
		return ex(what,
			"The tx breaks a protocol rule ("+rule+"), so it can never be valid in a block. A pool that "+
				"keeps it wastes space and invites a builder bug.",
			"Only rejected. The error text may differ between clients.")
	}
)

// exact holds the explanations of checks with a fixed name.
var exact = map[string]*Explanation{
	// Invariants.
	"Invariant: A built payload matches its build attributes": ex(
		"Every payload from getPayload has the timestamp, prevRandao, fee recipient, withdrawals and parent "+
			"that the forkchoiceUpdated attributes asked for.",
		"The Engine API requires it. A CL signs and broadcasts this payload; a mismatch makes the block "+
			"invalid or credits the wrong fee recipient.",
		"Always a match."),
	"Invariant: No tx appears twice in a payload": ex(
		"No tx hash appears more than once in a built payload.",
		"The second copy would reuse a nonce, so the block is invalid.",
		"Always zero duplicates."),
	"Invariant: A tx that must never be included is not in a payload": ex(
		"No built payload holds a tx the scenario marked as never includable (invalid, replaced, its nonce "+
			"already used, or its sender emptied).",
		"Such a tx makes the block invalid or runs something the sender already replaced.",
		"Always none. The detail names the tx and why it was marked."),
	"Invariant: A payload stays within the gas, blob gas and per-tx gas limits": ex(
		"Gas used is at most the gas limit, blob gas at most the fork's blob limit, and every tx's gas limit at "+
			"most the EIP-7825 cap (2^24).",
		"These are consensus limits; a payload over them is invalid.",
		"Always within."),
	"Invariant: A payload's blobs bundle matches its blob txs and its cell proofs verify": ex(
		"The blobs bundle of getPayload has one blob and commitment per versioned hash of the payload's blob "+
			"txs, in order, 128 cell proofs per blob, and the proofs verify with KZG.",
		"The CL publishes these blobs and proofs for data availability (PeerDAS). A wrong bundle makes the "+
			"block unavailable to the network.",
		"Always valid."),
	"Invariant: Every built payload is valid (newPayload)": ex(
		"Every payload the client builds passes its own newPayload. (Off in scenarios that time imports.)",
		"A builder that produces invalid blocks from its pool makes its proposer miss the slot.",
		"Always VALID."),
	"Invariant: After newPayload and forkchoiceUpdated, the head is the new block": ex(
		"After newPayload returns VALID and forkchoiceUpdated sets the block as head, eth_getBlockByNumber "+
			"(\"latest\") returns it.",
		"The pool resets against the head; a head that did not move breaks every later pool check.",
		"Always the new block."),
	"Invariant: getPayload's blockValue equals the priority fees paid": ex(
		"The blockValue of getPayload equals the sum of (effective gas price - base fee) x gas used over the "+
			"block's receipts.",
		"CLs and MEV-Boost compare local and relay blocks by blockValue. A wrong value picks the wrong block.",
		"Always equal."),
	"Invariant: Included txs leave the pool": ex(
		"A tx mined on the canonical chain is no longer listed by the pool, within 3 s of the import (txs "+
			"that a reorg brought back do not count).",
		"A pool that keeps mined txs offers them again: it wastes space and can make a builder pick a used nonce.",
		"Always gone. A short delay is fine: some clients reset the pool in the background."),
	"Invariant: eth_getTransactionByHash returns the tx that was sent": ex(
		"For a tx in the pool, eth_getTransactionByHash returns the same tx (same hash, type and nonce).",
		"Wallets and explorers use it to track their txs.",
		"Always the same tx."),
	"Invariant: txpool_status agrees with txpool_content": ex(
		"The pending and queued counts of txpool_status match the txs listed by txpool_content (blob txs may "+
			"be counted but not listed).",
		"Tools read either one. The two calls are not atomic, so a short mismatch can happen.",
		"Equal is expected; a difference is a finding to look at, not a bug."),
	"Invariant: The pool holds no tx below its sender's nonce": ex(
		"After the pool settles, no listed tx has a nonce lower than its sender's account nonce.",
		"Such a tx can never be valid again. A pool that keeps it is out of sync with the chain.",
		"Always none."),
	"Invariant: Each sender's pending txs are contiguous from its account nonce": ex(
		"After the pool settles, each sender's pending txs have the nonces n, n+1, n+2, ... from the account "+
			"nonce n, with no gap.",
		"Pending means executable now. A tx after a gap cannot execute, so it must be queued.",
		"Always contiguous."),

	// admission-rejections.
	"A valid control tx is accepted": ex(
		"A normal, valid tx from the same setup is accepted before the invalid ones are sent.",
		"Without it, every rejection below could be a broken setup, not a rejection.",
		"Only accepted."),
	"A duplicate submission does not add a second pool entry": ex(
		"Sending a tx that is already in the pool does not change the pool size.",
		"A pool keyed by hash must not hold two copies; a second copy could be broadcast or built twice.",
		"Pool size unchanged. The RPC answer (\"already known\" or the hash) may differ."),
	"No invalid tx is in the built payload": exNeverIncluded(
		"The payload built after all the invalid txs holds none of those that break a protocol rule."),
	"The valid control tx is in the built payload": ex(
		"The payload built after the invalid txs holds the valid control tx.",
		"It shows the builder works, so an empty payload is not why no invalid tx was included.",
		"Only included."),

	// nonce-gap-promotion.
	"Txs after a nonce gap are not pending": ex(
		"Txs with nonces n+1..n+8 (nonce n missing) are not listed as pending.",
		"They cannot execute until nonce n arrives. Pending ones would be offered to the builder.",
		"Only 0 pending."),
	"Txs after a nonce gap are queued": ex(
		"The same gapped txs are listed as queued.",
		"Queued is the common place to keep future txs, but a pool may also drop them.",
		"All queued is expected; fewer is a finding (policy)."),
	"Gapped txs are not in a built payload": exNeverIncluded("A payload built while the gap exists holds none of the gapped txs."),
	"Filling the gap promotes every queued tx": ex(
		"After nonce n arrives, all of n..n+8 become pending (timed).",
		"Promotion is how a pool makes waiting txs executable; a pool that does not promote loses them.",
		"Only all pending."),
	"Txs sent in reverse nonce order all become pending": ex(
		"Nonces sent from highest to lowest all end up pending once the lowest arrives.",
		"Txs arrive out of order on a network.",
		"Only all pending."),
	"The pool admits only the txs the balance can pay for": ex(
		"An account funded for 3 txs sends 6; the pool counts the cost of all its pending txs against the "+
			"balance.",
		"Without this check, an account with little ETH can fill many pool slots with txs that can never run.",
		"3 accepted is the expected strict policy; more accepted is a finding (some pools check each tx alone)."),
	"The pending nonce with a nonce gap: the gap": ex(
		"eth_getTransactionCount(addr, \"pending\") returns the first missing nonce while later txs are queued.",
		"Wallets use the pending nonce to number the next tx. A nonce past the gap makes the wallet skip it, "+
			"and the sender's txs stall.",
		"The gap is expected; another value is a finding (the \"pending\" tag is not specified exactly)."),
	"The pending nonce after the gap fills: past the last pending tx": ex(
		"After promotion, the pending nonce is one past the last pending tx.",
		"Same reason: wallets build the next tx on it.",
		"Expected as stated; another value is a finding."),
	"The pending nonce after mining: equal to the latest nonce": ex(
		"After every tx is mined and the pool is empty for this sender, the pending nonce equals the latest nonce.",
		"A stale pending nonce makes wallets reuse or skip nonces.",
		"Equal is expected; another value is a finding."),
	"After mining, the latest nonce counts every mined tx": ex(
		"eth_getTransactionCount(addr, \"latest\") equals the number of mined txs of the sender.",
		"This is chain state, not pool state.",
		"Only the exact count."),
	"The first value tx (8 of 10 ETH) is accepted": ex(
		"An account with 10 ETH sends a tx with 8 ETH value.",
		"It is valid; it sets up the next check.",
		"Only accepted."),
	"The pool counts value across a sender's txs (5 ETH after 8 ETH)": exPolicyReject(
		"With 8 ETH pending, the same account sends 5 ETH: together they exceed the 10 ETH balance.",
		"A pool that checks each tx alone accepts it; it can then never run after the 8 ETH tx."),
	"A tx the balance cannot pay after the first is never included": exNeverIncluded(
		"The 5 ETH tx is not in the payload that includes the 8 ETH tx."),

	// replacement.
	"The pool drops later nonces the replacement made unpayable": ex(
		"After nonce 1 of a 4-tx chain is replaced by a tx that spends the rest of the balance, the pool "+
			"drops nonces 2 and 3.",
		"They can never be paid. Keeping them holds pool space until something evicts them.",
		"Dropped is expected; kept is a finding (they must still never be included)."),
	"Unpayable later nonces are never in a payload": exNeverIncluded(
		"Nonces 2 and 3, unpayable after the replacement, are not in the payload with nonces 0 and 1."),
	"The payload with the replacement is valid": ex(
		"The payload with the chain start and the replacement imports as VALID.",
		"A builder that miscounts the balance after the replacement builds an invalid block.",
		"Only VALID."),

	// payload-ordering.
	"Txs of one sender are in nonce order, without gaps": ex(
		"In a built payload, each sender's txs appear in nonce order and none is skipped.",
		"A later nonce before an earlier one, or after a gap, makes the block invalid.",
		"Always in order."),
	"Full block: txs of one sender are in nonce order, without gaps": ex(
		"The same rule for a payload filled to the gas limit.",
		"Ordering under pressure is where a builder breaks it.",
		"Always in order."),
	"Txs are ordered by effective tip (nonce order kept)": ex(
		"The payload follows the greedy order: at each step the sender whose next tx pays the highest "+
			"effective tip.",
		"It is the common way to maximise fees, but builders may use other orders.",
		"The greedy order is expected; another order is a finding (policy)."),
	"A payload from a full pool fills the gas limit": ex(
		"With more txs in the pool than fit, the payload uses all but less than one tx of the gas limit.",
		"A half-empty block from a full pool loses fees and throughput.",
		"Full is expected; less is a finding."),
	"A payload holds at most the blob limit": ex(
		"A payload from a pool with more blob txs than fit holds at most the fork's blob limit.",
		"More blobs make the block invalid.",
		"Only at most the limit."),
	"A payload from a full blob pool fills the blob limit": ex(
		"With more blob txs than fit, the payload holds exactly the blob limit.",
		"Fewer blobs lose fees and data space.",
		"Full is expected; fewer is a finding."),
	"A tx that reverts is included": ex(
		"A call to a contract that always reverts is included in a block.",
		"A revert is a valid result: the tx pays gas and uses its nonce. It is not a reason to keep it out.",
		"Only included."),
	"The reverted tx has receipt status 0": ex(
		"Its receipt has status 0 (failed).",
		"Chain state: the call failed.",
		"Only 0."),
	"A reverted tx uses its nonce": ex(
		"The sender's nonce goes up by one after the reverted tx.",
		"Chain state: a failed tx still uses its nonce.",
		"Only nonce + 1."),
	"An included reverted tx leaves the pool": ex(
		"The reverted tx is no longer in the pool after its block.",
		"A pool must not keep it because execution failed: its nonce is used.",
		"Only gone."),
	"The next nonce after a reverted tx is accepted": ex(
		"The sender's next tx is accepted after the reverted one.",
		"A pool must not block a sender because its last tx failed.",
		"Accepted is expected; rejected is a finding."),

	// import-full-pool.
	"The measured block holds exactly the block txs": ex(
		"The block the scenario times holds exactly the 200 block txs (the filler arrives after the build).",
		"Otherwise the empty-pool and full-pool imports would not compare the same block shape.",
		"Only exactly the block txs; else the measurement is void."),
	"The pool settles to the filler count after the import": ex(
		"After the import, the pending count drops to the filler count: the block txs left, the filler stayed.",
		"This is the end of the timed pool reset.",
		"Only the filler count (within the timeout)."),
	"Included txs leave the pool": ex(
		"The block txs are no longer listed by the pool after the import.",
		"A pool that keeps mined txs offers used nonces to the builder.",
		"Only gone."),

	// reorg-reinjection.
	"Txs included in B1 leave the pool": ex(
		"After the head moves from A1 to B1, the txs that B1 includes are no longer in the pool.",
		"They are mined on the new canonical chain.",
		"Only gone."),
	"A-only txs come back to the pool after the reorg": ex(
		"Txs that were only in A1 (still valid on B1) are back in the pool after the reorg.",
		"Otherwise a reorg silently drops users' txs.",
		"Back is expected; missing is a finding (some pools do not reinject)."),
	"A tx that B1 made invalid (same nonce) is not in the pool": ex(
		"A1's tx T, whose nonce B1 used with another tx, is not reinjected.",
		"T can never be valid on B1.",
		"Only not in the pool."),
	"Pending txs from neither block stay in the pool": ex(
		"Txs sent while A1 was the head, and in neither block, are still pending after the reorg.",
		"A reorg must not drop unrelated txs.",
		"Kept is expected; lost is a finding."),
	"The invalidated tx is not in the next payload": exNeverIncluded("T is not in the payload built on B1."),
	"The next payload includes the reinjected txs": ex(
		"The payload built on B1 holds the reinjected A-only txs.",
		"Reinjected txs that never get built are as good as lost.",
		"Included is expected; missing is a finding."),
	"The next block on B1 is valid": ex(
		"The block built on B1 after the reorg imports as VALID.",
		"A pool that reinjects invalid txs makes the builder fail here.",
		"Only VALID."),

	// eviction-limits.
	"The pool stays within global_slots + global_queue": ex(
		"After 6400 low-tip txs, the pool holds at most global_slots + global_queue (6144) txs.",
		"The pinned limits are the configured cap. Pools that limit by memory (besu) do not count txs.",
		"Within the cap is expected; above it is a finding (check the client's limit model)."),
	"A full pool accepts txs that pay more than it holds": ex(
		"With the pool full of 1-1.39 gwei tips, txs with a 3 gwei tip are accepted.",
		"A full pool that refuses better txs lets cheap spam block real traffic.",
		"Accepted is expected; rejections are a finding."),
	"Higher-tip txs stay in a full pool": ex(
		"The accepted 3 gwei txs are still in the pool after it settles (cheaper ones were evicted for them).",
		"Same reason: eviction should drop the cheapest txs first.",
		"All kept is expected; fewer is a finding."),
	"A full pool does not keep txs cheaper than all it holds": ex(
		"Txs with a 0.5 gwei tip, cheaper than everything in a full pool, are not kept. Skipped if the pool "+
			"never filled.",
		"Keeping them would mean evicting better txs.",
		"Rejected or evicted is expected; kept is a finding."),
	"A payload from a full pool is valid": ex(
		"The payload built from the full pool imports as VALID.",
		"Eviction must not leave the builder with broken sender sequences.",
		"Only VALID."),
	"One sender's future txs stay within the account queue": ex(
		"One sender sends account_queue + 16 future txs; the pool keeps at most account_queue of them.",
		"A per-account queue cap stops one sender from filling the queue.",
		"At most the cap is expected; more is a finding (some clients have no per-account queue cap)."),
	"A gapped flood stays within global_queue": ex(
		"64 senders send only txs after a nonce gap; the pool keeps at most global_queue of them.",
		"Gapped txs can never run until the gap fills; without a cap they fill the pool for free.",
		"At most the cap is expected; more is a finding."),
	"Gapped txs are never in a payload": exNeverIncluded("No gapped tx is in the payload built during the flood."),

	// base-fee-movement.
	"Txs priced out by the base fee are never in a payload": exNeverIncluded(
		"Txs with a fee cap below the block's base fee are not in any payload while that holds."),
	"The pool keeps txs while the base fee prices them out": ex(
		"Txs with a fee cap below the current base fee stay in the pool through full blocks.",
		"The base fee can fall again; dropping them makes users resend.",
		"Kept is expected; dropped is a finding (policy)."),
	"Txs are included in the first block where they are valid": ex(
		"Once the base fee falls to their fee cap, the txs are in the first such block.",
		"A pool that is slow to re-check priced-out txs delays them for no reason.",
		"The first block is expected; later is a finding."),

	// sustained-load.
	"Valid txs are accepted under sustained load": ex(
		"No valid tx is rejected while 200 tx/s arrive and a block is built every slot.",
		"Rejections under normal load drop users' txs.",
		"0 rejected is expected; any rejection is a finding."),
	"Every accepted tx ends up in a block": ex(
		"Each tx accepted during the load is in a block by the end.",
		"Accepted txs that are never built are silently lost.",
		"All included is expected; leftovers are a finding."),

	// setcode-delegation.
	"A priced-out tx is not in the payload": exNeverIncluded(
		"Y's tx, priced just below the base fee to keep it pending, is not in the payload with the set-code tx."),
	"A valid authorization moves the authority's nonce": ex(
		"After the set-code tx runs, Y's nonce went up by one (EIP-7702 increments the authority's nonce).",
		"Chain state. It is why Y's pending tx at that nonce becomes invalid.",
		"Only nonce + 1."),
	"The pool drops a tx whose nonce an authorization used": ex(
		"Y's pending tx, whose nonce the authorization used, leaves the pool.",
		"It can never be valid again. (A pool may instead refuse the set-code tx up front, as nethermind does; "+
			"then this check does not run.)",
		"Dropped is expected; kept is a finding (it must still never be included)."),
	"A tx invalidated by an authorization is never in a payload": exNeverIncluded(
		"Y's tx is not in any payload, even after the base fee falls so its price would be valid."),
	"Delegating to the zero address clears the code": ex(
		"An authorization to the zero address removes the delegation: the account has no code after it.",
		"EIP-7702 defines this as the way to revoke.",
		"Only no code."),
	"After revoking, the account may have pending txs like a plain account": ex(
		"After the revoke, the account can have 3 pending txs again (while delegated, pools allow 1).",
		"A pool that keeps the delegated limit after a revoke penalises the account for nothing.",
		"3 accepted is expected; fewer is a finding."),
	"A set-code tx with some invalid authorizations is accepted": ex(
		"A set-code tx with three invalid authorizations and one valid one is accepted.",
		"The tx is valid: invalid authorizations are skipped at execution, they do not make the tx invalid.",
		"Accepted is expected; rejected is a finding (stricter policy)."),
	"The block with invalid authorizations is valid": ex(
		"The block with that set-code tx imports as VALID.",
		"Skipping invalid authorizations must not break the block.",
		"Only VALID."),

	// blob-pool.
	"A payload's blobs bundle matches its blob txs": ex(
		"The bundle has as many commitments as the payload's blob txs have blobs.",
		"A mismatch makes the blobs unavailable to the network.",
		"Only a match."),
	"Blob txs of an orphaned block come back to the pool": ex(
		"After a reorg removes the block with 3 blob txs, the pool has them again.",
		"To reinject a blob tx, the pool must still have its sidecar. A pool that dropped it loses the tx.",
		"Back is expected; missing is a finding."),
	"The next payload includes the reinjected blob txs (sidecars kept)": ex(
		"The payload built after the reorg holds the reinjected blob txs.",
		"It proves the sidecars are really there, not only the tx bodies.",
		"Included is expected; missing is a finding."),
	"After the reorg, the payload's blobs bundle matches its blob txs": ex(
		"The bundle of that payload matches its blob txs.",
		"Reinjected blobs must be the right ones.",
		"Only a match."),
	"The payload with reinjected blob txs is valid": ex(
		"That payload imports as VALID.",
		"Reinjected blob txs must not break the block.",
		"Only VALID."),
	"A blob tx priced out by the blob base fee is never in a payload": exNeverIncluded(
		"A blob tx whose max fee per blob gas is below the blob base fee is not in any payload."),

	// balance-changes.
	"A tx from an account with no balance is rejected": exPolicyReject(
		"An account with 0 ETH sends a tx.",
		"Its balance can rise later (as the next step shows)."),
	"A withdrawal credits the account": ex(
		"A withdrawal of 1 ETH in a block raises the account balance by exactly 1 ETH.",
		"Chain state (EIP-4895).",
		"Only exactly 1 ETH."),
	"The next payload includes the newly payable txs": ex(
		"After the withdrawal, the account's txs are accepted and the next payload holds them.",
		"A pool must re-check balances after a new head, not only on arrival.",
		"Included is expected; missing is a finding."),
	"The sweeper empties the delegated account": ex(
		"A call to the account (delegated to a sweeper contract) sends its whole balance to the caller.",
		"Chain state. It is how the account's balance falls without a tx from its owner.",
		"Only 0 wei."),
	"The pool drops txs whose sender was emptied": ex(
		"The emptied account's pending txs leave the pool.",
		"They can never be paid. Keeping them lets anyone fill pools with dead txs by draining delegated "+
			"accounts.",
		"Dropped is expected; kept is a finding (they must still never be included)."),
	"Txs of an emptied account are never in a payload": exNeverIncluded(
		"The emptied account's txs are not in any payload, even after the base fee falls."),
}

// rejectionExplanations explain the "Rejects: <case>" checks by case label.
var rejectionExplanations = map[string]*Explanation{
	"Nonce below the account nonce":       exProtocolReject("A tx with a nonce already used by a mined tx.", "nonce = account nonce"),
	"Gas limit below the intrinsic gas":   exProtocolReject("A tx with a gas limit of 20999 (a transfer needs 21000).", "gas limit >= intrinsic gas"),
	"Gas limit above the EIP-7825 cap":    exProtocolReject("A tx with a gas limit of 2^24 + 1.", "EIP-7825 per-tx gas cap"),
	"Gas limit above the block gas limit": exProtocolReject("A tx with a gas limit above the block gas limit.", "gas limit <= block gas limit, and the EIP-7825 cap"),
	"Wrong chain id":                      exProtocolReject("A tx signed for chain id + 1.", "EIP-155 chain id"),
	"maxPriorityFeePerGas above maxFeePerGas": exProtocolReject("A dynamic-fee tx with a tip above its fee cap.",
		"EIP-1559: tip <= fee cap"),
	"Value above the sender balance": exPolicyReject("A tx with a value far above the sender's balance.",
		"The balance can rise later."),
	"Contract creation with initcode above 49152 bytes": exProtocolReject("A creation with 49153 bytes of initcode.",
		"EIP-3860 initcode limit"),
	"Blob tx with a version 0 sidecar (no cell proofs) after Osaka": ex(
		"A blob tx whose sidecar has one KZG proof per blob (version 0) instead of cell proofs, after Osaka.",
		"The sidecar is not part of the block, so a client may convert it to cell proofs (geth does, at a CPU "+
			"cost) or refuse it. Both keep the network correct.",
		"Rejected or converted are both fine; record which. The time to convert is a metric."),
	"Blob tx with no blobs": exProtocolReject("A blob tx with an empty list of blob hashes.",
		"EIP-4844: at least one blob"),
	"Set-code tx with an empty authorization list": exProtocolReject("A set-code tx with no authorizations.",
		"EIP-7702: at least one authorization"),
	"Tx above 128 KiB": exPolicyReject("A tx of 132 KiB.",
		"The 128 KiB limit is a pool and network limit, not a consensus rule."),
	"Gas price below the pool minimum (0 wei)": exPolicyReject("A legacy tx with a gas price of 0.",
		"The minimum price is pool configuration; the profile sets it to 1 wei."),
	"Signature that recovers to no sender": exProtocolReject("A tx whose signature has r = 0.",
		"a valid secp256k1 signature"),
	"Blob tx with 7 blobs (Osaka allows 6 per tx)": exProtocolReject("A blob tx with 7 blobs.",
		"EIP-7594: at most 6 blobs per tx"),
	"Blob tx whose cell proofs do not match its commitments": exProtocolReject(
		"A blob tx whose cell proofs belong to other blobs.", "the sidecar's KZG proofs must verify"),
	"Blob tx whose blob fee the balance cannot pay": exPolicyReject(
		"A blob tx whose max blob fee (131072 blob gas x 1e19 wei) is more than the balance.",
		"The balance can rise later."),
	"Truncated RLP encoding": exProtocolReject("Bytes that are a tx encoding with the last 10 bytes cut.",
		"a decodable encoding"),
	"Unknown tx type 0x0f": exProtocolReject("A tx encoding with the unknown type byte 0x0f.",
		"only defined tx types"),
}

// replacementExplanations explain the "Replacement: <case>" checks.
var replacementExplanations = map[string]*Explanation{
	"Same fees, different content": ex(
		"A tx with the same sender, nonce and fees as one in the pool, but other content.",
		"Without a fee increase, anyone could swap their pending tx for free and spam the network.",
		"Rejected. Accepted is a finding."),
	"Fee cap and tip +5% (below the 10% bump)": ex(
		"The same nonce with fee cap and tip both 5% higher; the price bump is 10%.",
		"Below the bump, a replacement is too cheap to be worth relaying.",
		"Rejected. Accepted is a finding."),
	"Fee cap and tip +10%": ex(
		"Fee cap and tip both exactly 10% higher (the pinned price bump).",
		"Whether exactly 10% is enough or more than 10% is needed is client policy.",
		"Accepted is expected (geth); rejected at exactly the bump is a finding (besu wants more)."),
	"Fee cap and tip +100%": ex(
		"Fee cap and tip both doubled.",
		"Well above any bump rule.",
		"Accepted. Rejected is a finding."),
	"Only the fee cap +10%": ex(
		"Only the fee cap rises by 10%; the tip stays.",
		"Pools usually require both fields to rise, else the miner earns nothing more.",
		"Rejected is expected; accepted is a finding (policy)."),
	"Only the tip +10%": ex(
		"Only the tip rises by 10%; the fee cap stays.",
		"Same: both fields must usually rise.",
		"Rejected is expected; accepted is a finding."),
	"Legacy replaced by dynamic-fee, gas price +10% as fee cap and tip": ex(
		"A legacy tx replaced by a dynamic-fee tx whose fee cap and tip are both 10% above the old gas price.",
		"A legacy gas price is both its fee cap and its tip, so this is a valid bump across types.",
		"Accepted is expected; rejected is a finding (some pools do not compare across types)."),
	"Queued tx, fee cap and tip +10%": ex(
		"The same bump for a tx that waits behind a nonce gap.",
		"Replacement rules should not depend on pending or queued.",
		"Accepted is expected; a different answer than for a pending tx is a finding."),
	"Blob tx, all fees +10% (below the 100% blob bump)": ex(
		"A blob tx with fee cap, tip and blob fee cap 10% higher; the blob price bump is 100%.",
		"Blob txs are expensive to relay, so pools require a much larger bump.",
		"Rejected. Accepted is a finding."),
	"Blob tx, all fees +100%": ex(
		"A blob tx with all three fee fields doubled.",
		"Meets the blob price bump.",
		"Accepted. Rejected is a finding."),
	"Blob tx replaced by a dynamic-fee tx, fees +100%": ex(
		"A pending blob tx replaced by a non-blob tx with the same nonce.",
		"Many pools keep blob and non-blob txs apart, so they refuse to mix them for one nonce.",
		"Rejected is expected; accepted is a finding (policy)."),
	"Blob tx, only maxFeePerBlobGas +100%": ex(
		"A blob tx with only its blob fee cap doubled.",
		"Most pools require the execution fees to rise too.",
		"Rejected is expected; accepted is a finding."),
}

// prefixExplanations cover checks whose name ends in a case label.
var prefixExplanations = []struct {
	prefix string
	exp    func(rest string) *Explanation
}{
	{"Rejects: ", func(rest string) *Explanation { return rejectionExplanations[rest] }},
	{"Replacement: ", func(rest string) *Explanation { return replacementExplanations[rest] }},
	{"Replaced tx leaves the pool: ", func(string) *Explanation {
		return ex("After an accepted replacement, the old tx is no longer in the pool.",
			"Two txs with one sender and nonce would let the builder pick the old one.",
			"Only gone.")
	}},
	{"Accepted replacement is in the pool: ", func(string) *Explanation {
		return ex("After an accepted replacement, the new tx is in the pool.",
			"An accepted tx that is not in the pool is silently lost.",
			"Only in the pool.")
	}},
	{"Original stays after a rejected replacement: ", func(string) *Explanation {
		return ex("After a rejected replacement, the original tx is still in the pool.",
			"A rejected replacement must not remove the tx it tried to replace.",
			"Only still in the pool.")
	}},
	{"Authorization with ", func(rest string) *Explanation {
		if strings.HasPrefix(rest, "chain id 0") {
			return ex("An authorization with chain id 0 (valid on any chain) applies: the account is delegated.",
				"EIP-7702 allows chain id 0. It is the control case next to the invalid ones.",
				"Only delegated.")
		}

		return ex("An authorization that is invalid ("+strings.SplitN(rest, ":", 2)[0]+") is skipped: the "+
			"account is not delegated.",
			"EIP-7702: an invalid authorization is skipped at execution; it must not apply.",
			"Only not delegated.")
	}},
	{"Invariant: engine_getBlobsV2 returns the blobs of pool txs", func(string) *Explanation {
		return ex("For blob txs in the pool, engine_getBlobsV2 returns every blob with 128 cell proofs that "+
			"verify with KZG.",
			"Since Fusaka, the CL gets blobs from the EL's pool to build and check data columns (PeerDAS). "+
				"Missing or wrong blobs slow down or break that.",
			"Always all blobs, proofs verifying.")
	}},
}

// kindExplanations explain the admission checks per tx type.
var kindSuffixes = []struct {
	suffix string
	exp    *Explanation
}{
	{": valid txs are accepted under load", ex(
		"Valid txs of this type sent by 8 concurrent senders are all accepted.",
		"Under the pinned limits and a normal load, a pool must not drop valid txs.",
		"0 rejected.")},
	{": valid txs are accepted", ex(
		"Valid txs of this type sent one at a time are all accepted.",
		"Under the pinned limits, a pool must not drop valid txs.",
		"0 rejected.")},
	{": accepted txs are in the pool", ex(
		"Every accepted tx of this type is in the pool afterwards.",
		"An accepted tx that is not in the pool is silently lost.",
		"All in the pool.")},
	{": a single tx is accepted", ex(
		"A single valid tx of this type is accepted.",
		"It is the start of the time-until-pending measurement.",
		"Accepted.")},
	{": a single tx becomes pending", ex(
		"A single accepted tx of this type becomes pending.",
		"It is the end of the time-until-pending measurement.",
		"Pending.")},
}

// Explain returns the explanation of a check, or nil if it has none.
func Explain(name string) *Explanation {
	if e, ok := exact[name]; ok {
		return e
	}

	for _, p := range prefixExplanations {
		if rest, ok := strings.CutPrefix(name, p.prefix); ok {
			return p.exp(rest)
		}
	}

	for _, k := range kindSuffixes {
		if strings.HasSuffix(name, k.suffix) {
			return k.exp
		}
	}

	return nil
}
