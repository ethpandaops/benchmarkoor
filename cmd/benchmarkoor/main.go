package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const logTimestampFormat = "2006-01-02T15:04:05.000Z"

// utcFormatter wraps a logrus formatter and converts timestamps to UTC.
type utcFormatter struct {
	formatter logrus.Formatter
}

func (f *utcFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	entry.Time = entry.Time.UTC()

	return f.formatter.Format(entry)
}

// consistentFormatter formats log lines as: "$prefix $TIMESTAMP $LEVEL | $msg $fields\n".
type consistentFormatter struct {
	prefix string // e.g. "🔵"
}

// shortLevel maps logrus levels to 4-character abbreviations.
var shortLevel = map[logrus.Level]string{
	logrus.TraceLevel: "TRAC",
	logrus.DebugLevel: "DEBG",
	logrus.InfoLevel:  "INFO",
	logrus.WarnLevel:  "WARN",
	logrus.ErrorLevel: "ERRO",
	logrus.FatalLevel: "FATL",
	logrus.PanicLevel: "PANC",
}

func (f *consistentFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	ts := entry.Time.UTC().Format(logTimestampFormat)
	level := shortLevel[entry.Level]

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s %s %s | %s", f.prefix, ts, level, entry.Message)

	// Append fields in sorted order.
	if len(entry.Data) > 0 {
		keys := make([]string, 0, len(entry.Data))
		for k := range entry.Data {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			fmt.Fprintf(&buf, " %s=%v", k, entry.Data[k])
		}
	}

	buf.WriteByte('\n')

	return buf.Bytes(), nil
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
	log.SetFormatter(&utcFormatter{
		formatter: &logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z",
		},
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
	SilenceUsage: true,
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
