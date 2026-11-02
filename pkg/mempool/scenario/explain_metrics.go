package scenario

import "strings"

// Metric and latency explanations. OK says how to read the number: what is
// good, and what to compare it with.

var metricExact = map[string]*Explanation{
	// admission-rejections, balance-changes, setcode, replacement.
	"v0_sidecar_accept": ex(
		"Time for the client to accept a blob tx with a version 0 sidecar, which it converts to cell proofs.",
		"Converting computes 128 cell proofs per blob; it is CPU work an attacker can make the node do.",
		"Lower is better. Only clients that convert record it (about 150 ms for one blob in geth)."),
	"drain.dropped": ex(
		"Time from the block that emptied the account until the pool dropped its pending txs.",
		"Until then, the pool holds txs that can never be paid.",
		"Lower is better. Absent when the pool keeps them (a finding)."),
	"withdrawal.to_payload": ex(
		"Time from the withdrawal block until the newly funded account's txs are in a built payload.",
		"It includes sending the txs; it shows the pool uses the new balance at once.",
		"Lower is better; around one build (a few hundred ms) is normal."),
	"inflight.delegated": ex(
		"How many pending txs the pool lets a delegated (EIP-7702) account have.",
		"A delegated account's balance can change without a tx from it, so pools limit what they trust.",
		"1 is the common policy."),
	"nonce_bump.dropped": ex(
		"Time until the pool drops a tx whose nonce an authorization used.",
		"Until then, the pool holds a tx that can never be valid.",
		"Lower is better."),
	"mid_chain.dropped": ex(
		"Time until the pool drops nonces made unpayable by a mid-chain replacement.",
		"Same: dead txs take pool space.",
		"Lower is better. Absent when the pool keeps them or refused the replacement."),

	// base-fee-movement.
	"blocks_to_include": ex(
		"Blocks from the first one where the priced-out txs are valid until all of them are included.",
		"A pool that is slow to re-check them delays users' txs.",
		"1 is ideal (included at once)."),
	"fall_blocks": ex(
		"Blocks of falling base fee before the priced-out txs became valid.",
		"It depends only on the fee math, so it should be equal on every client.",
		"Equal across clients; a difference points to a base fee bug."),
	"time_to_include": ex(
		"Time from the first valid build until all priced-out txs are included.",
		"Same as blocks_to_include, in time.",
		"Lower is better."),

	// blob-pool.
	"per_sender.kept": ex(
		"Blob txs the pool kept from one sender that sent 24.",
		"Blob pools cap txs per account to bound memory.",
		"Policy: 16 (geth) is common."),
	"reorg.reinjected": ex(
		"Time from the reorg until the orphaned block's blob txs are back in the pool.",
		"Until then, the blob txs (and their sidecars) are missing for the next proposer.",
		"Lower is better. Absent when the pool does not bring them back (a finding)."),

	// eviction-limits.
	"big.kept": ex(
		"Large (120 KiB) txs the pool kept of 200.",
		"Memory-limited pools keep fewer large txs than count-limited pools.",
		"All 200 is common at this size."),
	"fill.pending": ex(
		"Pending txs after 6400 low-tip txs were sent.",
		"It shows where the client's pending limit is.",
		"About global_slots (5120) or global_slots + global_queue (6144); more for memory-limited pools."),
	"fill.queued": ex(
		"Queued txs after the fill.",
		"The fill has no gaps, so few should be queued.",
		"0 or small."),
	"fill.rejected": ex(
		"Low-tip txs rejected during the fill.",
		"A full pool may reject the newest txs instead of evicting older ones.",
		"Policy; compare with low.evicted."),
	"high.kept": ex(
		"Share of the higher-tip txs still in the full pool.",
		"Eviction should drop the cheapest txs to make room for better ones.",
		"100% is expected; less is a finding."),
	"low.evicted": ex(
		"Low-tip txs evicted to make room for the higher-tip ones.",
		"It shows eviction works (and roughly how much).",
		"About the number of higher-tip txs (1280) for a full count-limited pool; 0 if the pool was not full."),
	"payload.high": ex(
		"Share of the higher-tip txs in the next payload.",
		"A builder should prefer them over cheaper txs.",
		"100% is expected (they fit in one block)."),
	"hog.kept": ex(
		"Txs kept from one sender that sent 256.",
		"A per-sender cap stops one account from taking the pool.",
		"Policy: 16 (hard cap), 64 or 256 are all seen."),
	"queue.kept": ex(
		"Future-nonce txs kept from one sender that sent 80 (account queue 64).",
		"A per-account queue cap stops one sender from filling the queue.",
		"64 (the pinned cap); 80 for clients without a per-account cap."),

	// import-full-pool.
	"fcu.empty":            exImport("forkchoiceUpdated after newPayload", "empty pool"),
	"fcu.full":             exImport("forkchoiceUpdated after newPayload", "full pool"),
	"new_payload.empty":    exImport("newPayload", "empty pool"),
	"new_payload.full":     exImport("newPayload", "full pool"),
	"settle.empty":         exImport("time until the pool settles after the head change", "empty pool"),
	"settle.full":          exImport("time until the pool settles after the head change", "full pool"),
	"fcu.overhead":         exOverhead("forkchoiceUpdated"),
	"new_payload.overhead": exOverhead("newPayload"),
	"settle.overhead":      exOverhead("the pool reset after the head change"),

	// nonce-gap-promotion.
	"reverse_order_pending": ex(
		"Time until 8 txs sent in reverse nonce order are all pending.",
		"Out-of-order arrival is normal on a network; promotion must be quick.",
		"Lower is better."),

	// payload-latency.
	"inclusion": ex(
		"Share of the txs sent during a 4 s build window that are in the payload delivered at its end.",
		"A builder that adds new txs while it builds serves fresher blocks.",
		"Higher is better. It follows the builder's rebuild interval (geth: 2 s, so about 50%)."),

	// payload-ordering.
	"blob_get_payload":  exCall("getPayload of a payload with blobs"),
	"build_fcu":         exCall("forkchoiceUpdated with build attributes (one call)"),
	"build_get_payload": exCall("getPayload of a normal payload (one call)"),
	"fill_get_payload":  exCall("getPayload of a full block"),
	"fill_ratio": ex(
		"Gas used / gas limit of a payload built from a pool with more txs than fit.",
		"A builder should fill the block when it can.",
		"Close to 100% is expected."),
	"fill_txs": ex(
		"Txs in that full payload.",
		"With 21000-gas transfers and a 30M limit, about 1428 fit.",
		"About 1428."),

	// sustained-load.
	"blocks": ex("Blocks built during the load and the drain after it.", "Context for the other numbers.",
		"About duration / slot, plus a few."),
	"rate": ex("Accepted txs per second during the load.", "It shows the pool keeps up with the send rate.",
		"Close to the configured rate (200 tx/s)."),
	"rejected": ex("Sends rejected during the load.", "Valid txs must not be rejected at normal load.",
		"0 is expected."),
	"txs_per_block": ex("Mean txs per block during the load.", "rate x slot is the most a block can hold.",
		"About 400 at 200 tx/s and 2 s slots."),
}

// metricPrefixes explain the admission metrics per tx type.
var metricPrefixes = []struct {
	prefix string
	exp    *Explanation
}{
	{"serial_tps.", ex(
		"Accepted txs per second with one eth_sendRawTransaction at a time.",
		"It is the client's admission cost per tx (validation + RPC), without concurrency.",
		"Higher is better. Noisy for JIT clients (besu); compare over repeated runs.")},
	{"concurrent_tps.", ex(
		"Accepted txs per second with 8 concurrent senders.",
		"It shows how well admission scales with parallel callers.",
		"Higher is better; usually well above the serial number.")},
	{"all_in_pool.", ex(
		"Time from the first serial send until every tx of the batch is in the pool.",
		"It includes the sends; it shows no tx lags behind.",
		"Lower is better.")},
}

// latencyExact explain latency series by key.
var latencyExact = map[string]*Explanation{
	"reject_latency": ex("Time for eth_sendRawTransaction to reject an invalid tx (rejections only).",
		"Cheap rejection keeps invalid spam cheap for the node.", "Lower is better; well under 1 ms is common."),
	"send.big": ex("eth_sendRawTransaction time for a 120 KiB tx.", "Large txs cost more to decode and hash.",
		"Lower is better."),
	"send.fill": ex("eth_sendRawTransaction time while the pool fills up.", "Admission should not slow down as "+
		"the pool grows.", "Compare with send.full."),
	"send.full": ex("eth_sendRawTransaction time into a full pool (with eviction).",
		"Eviction adds work per tx.", "Somewhat above send.fill is normal."),
	"promotion": ex("Time from the gap-filling tx until all 8 queued txs are pending.",
		"Promotion runs when the missing nonce arrives.", "Lower is better; a few ms is common."),
	"get_payload": ex("getPayload time at the end of the build window.",
		"The CL waits for it at the start of its slot.", "Lower is better."),
	"staleness": ex("Age of the newest included tx when the payload is delivered.",
		"Shows how fresh the builder's tx set is.",
		"Lower is better; it follows the rebuild interval (geth about 2000 ms)."),
	"reorg_fcu": ex("forkchoiceUpdated time for a 1-block reorg.", "Reorgs must be cheap.", "Lower is better."),
	"reorg_reinjected": ex("Time after the reorg until A-only txs are back in the pool.",
		"Until then, those txs are missing for the next proposer.", "Lower is better."),
	"reorg_removed": ex("Time after the reorg until the txs included in B1 leave the pool.",
		"Until then, the pool offers mined txs.", "Lower is better."),
	"reorg_settled": ex("Time after the reorg until both of the above are done.",
		"The pool is consistent again only then.", "Lower is better."),
	"replace_visible": ex("Time from an accepted replacement until the old tx is gone from the pool.",
		"Until then, the pool shows two txs for one nonce.", "Lower is better."),
	"fcu_build": ex("forkchoiceUpdated with build attributes, under load.",
		"Starting a build must stay cheap with a busy pool.", "Lower is better."),
	"fcu_head": ex("forkchoiceUpdated that sets the new head, under load.",
		"It includes the start of the pool reset in some clients.", "Lower is better."),
	"inclusion": ex("Delay from sending a tx until it is in the new head, under load.",
		"End-to-end time a user waits.",
		"About one slot for a builder that adds txs during the build window; up to two otherwise."),
	"new_payload": ex("newPayload time under load.", "Block import with a busy pool.", "Lower is better."),
	"send": ex("eth_sendRawTransaction time under sustained load.", "Admission must stay fast while blocks are "+
		"built.", "Lower is better; watch p99."),
	"fcu.empty":         exImportLatency("forkchoiceUpdated", "empty pool"),
	"fcu.full":          exImportLatency("forkchoiceUpdated", "full pool"),
	"new_payload.empty": exImportLatency("newPayload", "empty pool"),
	"new_payload.full":  exImportLatency("newPayload", "full pool"),
	"settle.empty":      exImportLatency("pool settle after the head change", "empty pool"),
	"settle.full":       exImportLatency("pool settle after the head change", "full pool"),
}

var latencyPrefixes = []struct {
	prefix string
	exp    *Explanation
}{
	{"send_concurrent.", ex("eth_sendRawTransaction time per call with 8 concurrent senders.",
		"Concurrency shows lock contention in admission.", "Lower is better; above the serial time is normal.")},
	{"send.", ex("eth_sendRawTransaction time per call, one at a time.",
		"Admission cost per tx of this type (blob txs verify KZG proofs).",
		"Lower is better; blob txs take far longer (proof checks).")},
	{"pending.", ex("Time from the send until the pool lists the tx as pending.",
		"Some pools add a tx as queued first and promote it in the background.", "Lower is better.")},
	{"in_pool.", ex("Time from the send until the tx is in the pool (the client has no pending view).",
		"Used instead of time-until-pending for such clients.", "Lower is better.")},
}

func exImport(what, pool string) *Explanation {
	return ex("Median "+what+" for a 200-tx block with an "+pool+".",
		"The import path resets the pool; a big pool can make it slower.",
		"Lower is better. Compare the empty and full values: the gap is the pool's cost.")
}

func exImportLatency(what, pool string) *Explanation {
	return ex(what+" for each round, "+pool+".",
		"The distribution behind the median.", "Lower is better; a wide spread means noise.")
}

func exOverhead(what string) *Explanation {
	return ex("How much slower "+what+" is with a full pool than with an empty one (medians).",
		"It is the pool's cost on the import path.",
		"Near 0% is best. Values of a few % either way are noise; a large positive value is a real cost.")
}

func exCall(what string) *Explanation {
	return ex(what+".", "A single call: useful as a hint, noisy as a number.", "Lower is better.")
}

// ExplainMetric returns the explanation of a metric key, or nil.
func ExplainMetric(key string) *Explanation {
	if e, ok := metricExact[key]; ok {
		return e
	}

	for _, p := range metricPrefixes {
		if strings.HasPrefix(key, p.prefix) {
			return p.exp
		}
	}

	return nil
}

// ExplainLatency returns the explanation of a latency key, or nil.
func ExplainLatency(key string) *Explanation {
	if e, ok := latencyExact[key]; ok {
		return e
	}

	for _, p := range latencyPrefixes {
		if strings.HasPrefix(key, p.prefix) {
			return p.exp
		}
	}

	return nil
}
