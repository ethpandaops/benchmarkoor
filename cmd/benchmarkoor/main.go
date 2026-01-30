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

// ANSI color codes for log levels.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
)

// levelLabel contains the 4-char abbreviation and ANSI color for a log level.
type levelLabel struct {
	text  string
	color string
}

// shortLevel maps logrus levels to colored 4-character abbreviations.
var shortLevel = map[logrus.Level]levelLabel{
	logrus.TraceLevel: {text: "TRAC", color: colorWhite},
	logrus.DebugLevel: {text: "DEBG", color: colorCyan},
	logrus.InfoLevel:  {text: "INFO", color: colorGreen},
	logrus.WarnLevel:  {text: "WARN", color: colorYellow},
	logrus.ErrorLevel: {text: "ERRO", color: colorRed},
	logrus.FatalLevel: {text: "FATL", color: colorRed},
	logrus.PanicLevel: {text: "PANC", color: colorRed},
}

func (f *consistentFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	ts := entry.Time.UTC().Format(logTimestampFormat)
	lbl := shortLevel[entry.Level]
	level := lbl.color + lbl.text + colorReset

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
