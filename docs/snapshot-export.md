# Snapshot export

`benchmarkoor snapshot export` publishes a **stopped** client datadir in the
[snapshots.ethpandaops.io](https://snapshots.ethpandaops.io) layout, so anything
that already reads those snapshots (ethereum-package's `network_sync_base_url`,
the `snapshot_fetcher` ansible role) can read it unchanged:

```
<prefix><network>/<client>/<block>/<archive>            see "Archive name"
<prefix><network>/<client>/<block>/_snapshot_eth_getBlockByNumber.json
<prefix><network>/<client>/<block>/_snapshot_metadata.json
<prefix><network>/<client>/latest                      "<block>\n", only with --write-latest
```

```sh
export S3_ENDPOINT_URL=https://<account>.r2.cloudflarestorage.com AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...
benchmarkoor snapshot export \
  --client geth --datadir /data/geth --network mainnet --block 26188082 \
  --bucket ethpandaops-shadowfork-images \
  --head-block-file head.json \
  --metadata-file metadata.json \
  --verify-public-base https://shadowfork-images.ethpandaops.io
```

| flag | |
|---|---|
| `--client` | `besu`, `erigon`, `ethrex`, `geth`, `nethermind`, `reth` |
| `--datadir` | the stopped datadir; for geth, the directory that contains `geth/` |
| `--network`, `--block` | the first and third key segments |
| `--bucket`, `--prefix` | destination; the prefix is prepended verbatim (`""` by default, include a trailing `/`) |
| `--head-block-file` | the `eth_getBlockByNumber(<block>, true)` JSON-RPC response, published as-is; its `result.number` must equal `--block` |
| `--metadata-file` | a JSON object merged over the generated metadata (e.g. `docker_image`, `static.extra_args`, `shadowfork.amsterdam_time`) |
| `--write-latest` | point `<network>/<client>/latest` at `--block`, written last |
| `--verify-public-base` | public URL of the bucket root; verification reads through it, as consumers do |
| `--verify-blocks` | random 1 GiB ranges to re-read (default 3) |
| `--zstd-level` | default 6 |
| `--archive-name` | overrides the client's archive name, see below |

Requires GNU `tar` and `zstd` on `PATH` (both are in the benchmarkoor image).
Run it as a user that can read every file in the datadir.

## What it does, in order

1. **Pack**: walks the datadir, drops the excludes below, and feeds the sorted
   member list to `tar --null --no-recursion -T -`, piped through
   `zstd -<level> -T0`. Members are in `tar --sort=name` order, so the same datadir
   always gives the same archive.
2. **Stream**: the zstd output goes straight into a multipart upload. No archive is
   written to disk. Parts are sized from the datadir: 64 MiB, grown until the archive
   fits in 9,000 parts (S3 allows 10,000), so a 1.5 TB datadir gets ~160 MiB parts.
   Eight parts are in flight, so memory use is eight times the part size. If tar or
   zstd fails (including tar's "file changed as we read it" from a datadir that is
   still running), the stream ends with a read error. The multipart upload is then
   **aborted**, never completed with a truncated archive.
3. **Hash**: the sha256 of every 1 GiB of the uploaded stream is recorded on the way
   through.
4. **Verify**: the object's size must match the bytes sent. Then N random 1 GiB
   ranges are re-read and compared with their hashes. With `--verify-public-base`,
   ranges are read through that URL and each one must come back as a `206`; without
   it, they are read through S3 with the bucket credentials.
5. **Head block and metadata**: written only after verification passes.
   `_snapshot_metadata.json` is `{"data_size_bytes": <datadir bytes>}` with
   `--metadata-file` merged over it, nested objects key by key.
6. **`latest`**: with `--write-latest` only, and last. A consumer of `latest` never
   sees a height whose files are not all up.

## Packing per client

| client | archive root | excluded |
|---|---|---|
| geth | the contents of `<datadir>/geth`, **no prefix**, as snapshots.ethpandaops.io publishes it: consumers extract into `<datadir>/geth`, where geth reads `triedb/merkle.journal` (extracted flat, geth opens `chaindata/` but misses the journal and rewinds its head) | |
| erigon | `./` | |
| reth | `./` | `discovery-secret`, `known-peers.json` |
| besu | `./` | `key` |
| ethrex | `./` | `node.key`, `node_config.json` |
| nethermind | the contents of `<datadir>/nethermind_db`, **no prefix**: `mainnet/` at the root, as jochemnet's tarball; consumers extract into `<datadir>/nethermind_db` and run `--Init.BaseDbPath=nethermind_db/mainnet` | `*/peers`, `*/discoveryNodes` |

Every client also excludes `nodekey`, `LOCK`, `nodes/`, `logs/`, `.download-cache/`, `.snapshot_fetcher_*`,
`download_snapshot.sh` (the snapshot downloader's) and `_snapshot_*` (a previous publish's files) from the top of its archive root. All excludes match
from the archive root, so a database's own `LOCK` (e.g. `chaindata/LOCK`) is
kept.

## Archive name

The names the `snapshot_fetcher` inventories (jochemnet, msf-2) fetch:

| client | archive |
|---|---|
| reth | `snapshot-v2.tar.zst` (reth's v2 storage; `snapshot.tar.zst` is the v1 format) |
| erigon | `snapshot-pruned.tar.zst` |
| everyone else | `snapshot.tar.zst` |

snapshots.ethpandaops.io's mainnet reth and erigon are under `snapshot.tar.zst`. To
publish to that name, pass `--archive-name snapshot.tar.zst`; the exporter cannot
tell a datadir's storage format from its files.

## Tests

`make test-core` covers the key layout, the excludes for every client, the packing
order, the block hashing and the verification (including an HTTP server that ignores
`Range`). `make test-integration-core` runs a throwaway MinIO in docker and checks
that:

- the archive spans several parts;
- it extracts to the datadir minus the excludes;
- a byte flipped in one part is caught by the range verification;
- `latest` is written only on request;
- a tar failure mid-upload leaves no object and no open multipart upload behind.
