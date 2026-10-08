package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/snapshot"
	"github.com/ethpandaops/benchmarkoor/pkg/upload"
	"github.com/spf13/cobra"
)

var snapshotExport struct {
	opts          snapshot.ExportOptions
	headBlockFile string
	metadataFile  string
}

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Work with client datadir snapshots",
}

var snapshotExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Publish a stopped datadir as a snapshot in the snapshots.ethpandaops.io layout",
	Long: `Streams a stopped client datadir as tar | zstd straight into a multipart
upload at <prefix><network>/<client>/<block>/<archive> (no local archive),
hashing every 1 GiB of the stream. The upload is then checked: its size, and
random 1 GiB ranges against those hashes, read through --verify-public-base
when given (each range must be a 206), else through S3. Only then are
_snapshot_eth_getBlockByNumber.json and _snapshot_metadata.json written, and
with --write-latest, <network>/<client>/latest last.

Credentials: S3_ENDPOINT_URL, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY.
Requires GNU tar and zstd on PATH.`,
	Args: cobra.NoArgs,
	RunE: runSnapshotExport,
}

func init() {
	rootCmd.AddCommand(snapshotCmd)
	snapshotCmd.AddCommand(snapshotExportCmd)

	o := &snapshotExport.opts
	f := snapshotExportCmd.Flags()
	f.StringVar(&o.Client, "client", "", "Client whose datadir this is: "+strings.Join(snapshot.Clients(), ", "))
	f.StringVar(&o.Datadir, "datadir", "", "Stopped datadir (geth: the directory containing geth/)")
	f.StringVar(&o.Network, "network", "", "Network name, the first key segment (e.g. mainnet)")
	f.Uint64Var(&o.Block, "block", 0, "Head block number of the datadir")
	f.StringVar(&o.Bucket, "bucket", "", "Destination bucket")
	f.StringVar(&o.Prefix, "prefix", "", "Key prefix prepended to <network>/... (include a trailing /)")
	f.StringVar(&snapshotExport.headBlockFile, "head-block-file", "",
		"eth_getBlockByNumber(<block>, true) JSON-RPC response, published as "+snapshot.HeadBlockName)
	f.StringVar(&snapshotExport.metadataFile, "metadata-file", "",
		"JSON object merged over the generated "+snapshot.MetadataName+" (e.g. docker_image, shadowfork.amsterdam_time)")
	f.BoolVar(&o.WriteLatest, "write-latest", false, "Point <network>/<client>/latest at --block, written last")
	f.StringVar(&o.VerifyPublicBase, "verify-public-base", "",
		"Public URL of the bucket root to verify through (e.g. https://snapshots.ethpandaops.io)")
	f.IntVar(&o.VerifyBlocks, "verify-blocks", 3, "Random 1 GiB ranges to re-read and compare")
	f.IntVar(&o.ZstdLevel, "zstd-level", 6, "zstd compression level (1-19)")
	f.StringVar(&o.Archive, "archive-name", "",
		"Archive object name (default: snapshot-v2.tar.zst for reth, snapshot-pruned.tar.zst for erigon, else snapshot.tar.zst)")

	for _, name := range []string{"client", "datadir", "network", "block", "bucket", "head-block-file"} {
		_ = snapshotExportCmd.MarkFlagRequired(name)
	}
}

func runSnapshotExport(cmd *cobra.Command, _ []string) error {
	o := snapshotExport.opts

	var err error
	if o.HeadBlock, err = os.ReadFile(snapshotExport.headBlockFile); err != nil {
		return fmt.Errorf("reading head block: %w", err)
	}

	if snapshotExport.metadataFile != "" {
		raw, err := os.ReadFile(snapshotExport.metadataFile)
		if err != nil {
			return fmt.Errorf("reading metadata: %w", err)
		}

		if err := json.Unmarshal(raw, &o.Metadata); err != nil {
			return fmt.Errorf("metadata file %s must be a JSON object: %w", snapshotExport.metadataFile, err)
		}
	}

	cfg := &config.S3UploadConfig{
		Bucket:          o.Bucket,
		EndpointURL:     os.Getenv("S3_ENDPOINT_URL"),
		Region:          os.Getenv("AWS_REGION"),
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		ForcePathStyle:  true,
	}
	if cfg.EndpointURL == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return fmt.Errorf("S3_ENDPOINT_URL, AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set")
	}

	// An interrupt kills tar and zstd and aborts the multipart upload.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res, err := snapshot.Export(ctx, log, upload.NewS3Client(cfg), o)
	if err != nil {
		return err
	}

	log.WithField("key", res.ArchiveKey).WithField("bytes", res.ArchiveBytes).
		WithField("data_bytes", res.DataBytes).Info("Snapshot published")

	return nil
}
