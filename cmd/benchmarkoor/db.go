package main

import (
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/runner"
	"github.com/spf13/cobra"
)

var dbCompactFlags struct {
	client    string
	datadir   string
	prepare   []string
	image     string
	extraArgs []string
	timeout   string
	noInspect bool
}

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Offline client database maintenance",
}

var dbCompactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Compact a stopped client's datadir, as db_compaction does in a run",
	Long: `Compact a stopped client's datadir using the client's own offline command.
Supported clients are geth and erigon. Use --image to select the same client
version that wrote the datadir. Other clients return an unsupported error.`,
	Args: cobra.NoArgs,
	RunE: runDBCompact,
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.AddCommand(dbCompactCmd)

	f := dbCompactCmd.Flags()
	f.StringVar(&dbCompactFlags.client, "client", "", "client that wrote the datadir (geth or erigon)")
	f.StringVar(&dbCompactFlags.datadir, "datadir", "", "host path of the datadir; the client must be stopped")
	f.StringArrayVar(&dbCompactFlags.prepare, "prepare", nil, "preparation step to run before the compaction (repeatable, e.g. erigon's seg-retire)")
	f.StringVar(&dbCompactFlags.image, "image", "", "client image (default: the client's default image)")
	f.StringArrayVar(&dbCompactFlags.extraArgs, "extra-arg", nil, "argument appended to the compaction command (repeatable)")
	f.StringVar(&dbCompactFlags.timeout, "timeout", config.DefaultDBCompactionTimeout, "time limit for the whole compaction")
	f.BoolVar(&dbCompactFlags.noInspect, "no-inspect", false, "skip the database inspection before and after")

	_ = dbCompactCmd.MarkFlagRequired("client")
	_ = dbCompactCmd.MarkFlagRequired("datadir")
}

func runDBCompact(cmd *cobra.Command, _ []string) error {
	ct := client.ClientType(dbCompactFlags.client)
	if !client.SupportsDBCompaction(ct) {
		return fmt.Errorf("client %q has no offline database compaction", ct)
	}

	if dbCompactFlags.datadir == "" {
		return fmt.Errorf("--datadir must not be empty")
	}

	timeout, err := time.ParseDuration(dbCompactFlags.timeout)
	if err != nil {
		return fmt.Errorf("--timeout: %w", err)
	}

	if timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}

	inspect := !dbCompactFlags.noInspect
	cfg := &config.DBCompactionConfig{
		Enabled:   true,
		Prepare:   dbCompactFlags.prepare,
		ExtraArgs: dbCompactFlags.extraArgs,
		Timeout:   dbCompactFlags.timeout,
		Inspect:   &inspect,
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mgr, err := docker.NewManager(log)
	if err != nil {
		return err
	}

	if err := mgr.Start(ctx); err != nil {
		return err
	}

	defer func() { _ = mgr.Stop() }()

	return runner.CompactDatadir(ctx, log, mgr, &runner.DatadirCompaction{
		Client:  ct,
		DataDir: dbCompactFlags.datadir,
		Image:   dbCompactFlags.image,
		Cfg:     cfg,
	})
}
