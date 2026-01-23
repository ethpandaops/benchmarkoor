package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// prefixedFormatter wraps a logrus formatter and adds a prefix to each line.
type prefixedFormatter struct {
	prefix    string
	formatter logrus.Formatter
}

func (f *prefixedFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	formatted, err := f.formatter.Format(entry)
	if err != nil {
		return nil, err
	}

	return append([]byte(f.prefix), formatted...), nil
}

var (
	// Version information set at build time.
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var (
	cfgFiles []string
	logLevel string
	log      *logrus.Logger
)

func main() {
	log = logrus.New()
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	if err := rootCmd.Execute(); err != nil {
		log.WithError(err).Fatal("Failed to execute command")
	}
}

var rootCmd = &cobra.Command{
	Use:   "benchmarkoor",
	Short: "Ethereum execution layer client benchmarking tool",
	Long: `Benchmarkoor is a tool for benchmarking Ethereum execution layer clients.
It supports running EL clients via Docker and measuring their performance
through the Engine API.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level, err := logrus.ParseLevel(logLevel)
		if err != nil {
			return fmt.Errorf("invalid log level %q: %w", logLevel, err)
		}

		log.SetLevel(level)

		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("benchmarkoor %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
	},
}

func init() {
	rootCmd.PersistentFlags().StringArrayVar(&cfgFiles, "config", nil,
		"config file path (can be specified multiple times)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		"log level ("+strings.Join(logLevels(), ", ")+")")

	rootCmd.AddCommand(versionCmd)
}

func logLevels() []string {
	levels := make([]string, 0, len(logrus.AllLevels))
	for _, level := range logrus.AllLevels {
		levels = append(levels, level.String())
	}

	return levels
}
