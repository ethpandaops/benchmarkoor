// Package iocost throttles a block device with the cgroup v2 io.cost
// controller. Unlike io.max, io.cost gives reads and writes one shared
// budget of device time, which follows the capacity of a real disk.
//
// io.cost is set in the root cgroup and applies to all I/O on the disk, not
// to one container. The manager saves the original settings before it
// changes them, and restores them at the end of the run.
package iocost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	// DefaultCgroupPath is the mount point of the cgroup v2 hierarchy.
	DefaultCgroupPath = "/sys/fs/cgroup"

	modelFile = "io.cost.model"
	qosFile   = "io.cost.qos"

	// freeBps and freeIOPS stand for a direction without a limit. The cost
	// of its I/O is then so low that it does not use the budget.
	freeBps  = 100 << 30
	freeIOPS = 10_000_000

	// qosFixedRate turns off the latency control and fixes the device rate
	// at 100% of the model. The model values are then hard limits.
	qosFixedRate = "enable=1 ctrl=user rpct=0 wpct=0 min=100 max=100"
)

// Model is the capacity of a reference disk. A zero value means that the
// direction has no limit.
type Model struct {
	ReadBps   uint64
	ReadIOPS  uint64
	WriteBps  uint64
	WriteIOPS uint64
}

// String returns the io.cost.model parameters of the linear model. The
// IOPS values apply to both sequential and random I/O.
func (m Model) String() string {
	rbps, riops := orFree(m.ReadBps, freeBps), orFree(m.ReadIOPS, freeIOPS)
	wbps, wiops := orFree(m.WriteBps, freeBps), orFree(m.WriteIOPS, freeIOPS)

	return fmt.Sprintf(
		"ctrl=user model=linear rbps=%d rseqiops=%d rrandiops=%d wbps=%d wseqiops=%d wrandiops=%d",
		rbps, riops, riops, wbps, wiops, wiops,
	)
}

func orFree(v, free uint64) uint64 {
	if v == 0 {
		return free
	}

	return v
}

// Manager applies io.cost models to block devices and restores them.
type Manager interface {
	Start(ctx context.Context) error
	// Stop restores all devices that still have a model of this manager.
	Stop() error
	// Apply sets model on the whole disk with the device number majMin
	// ("259:0"). It returns the io.cost.model line that it wrote.
	Apply(ctx context.Context, majMin string, model Model) (string, error)
	// Restore puts back the original settings of all changed devices.
	Restore(ctx context.Context) error
}

// NewManager creates a manager. cacheDir holds the state files that let
// "benchmarkoor cleanup" restore a device after a crash. cgroupPath is the
// cgroup v2 mount point.
func NewManager(log logrus.FieldLogger, cacheDir, cgroupPath string) Manager {
	if cgroupPath == "" {
		cgroupPath = DefaultCgroupPath
	}

	return &manager{
		log:        log.WithField("component", "iocost"),
		cacheDir:   cacheDir,
		cgroupPath: cgroupPath,
		originals:  make(map[string]DeviceSettings, 1),
	}
}

type manager struct {
	log        logrus.FieldLogger
	cacheDir   string
	cgroupPath string

	mu sync.Mutex
	// originals are the settings before the first Apply, per device.
	originals map[string]DeviceSettings
	stateFile string
}

var _ Manager = (*manager)(nil)

// DeviceSettings are the io.cost lines of one device. An empty line means
// that the device had no io.cost settings.
type DeviceSettings struct {
	MajMin string `json:"maj_min"`
	Model  string `json:"model,omitempty"`
	QoS    string `json:"qos,omitempty"`
}

// Start restores the devices of state files that an interrupted run left
// behind. Without this, Apply would save the throttle of that run as the
// original setting, and the disk would stay throttled after this run.
func (m *manager) Start(ctx context.Context) error {
	files, err := ListOrphanedStateFiles(m.cacheDir)
	if err != nil {
		return err
	}

	if len(files) > 0 {
		m.log.WithField("files", len(files)).Warn("Found io.cost state files of an interrupted run, restoring them")
	}

	return CleanupOrphanedState(ctx, m.log, files)
}

func (m *manager) Stop() error {
	return m.Restore(context.Background())
}

func (m *manager) Apply(_ context.Context, majMin string, model Model) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, known := m.originals[majMin]
	if !known {
		orig, err := readSettings(m.cgroupPath, majMin)
		if err != nil {
			return "", err
		}

		m.originals[majMin] = orig
		m.saveState()
	}

	modelLine := model.String()

	// Set the model before io.cost is on, so the device never runs with the
	// default model.
	if err := writeLine(m.cgroupPath, modelFile, majMin, modelLine); err != nil {
		// The kernel rejected the device, so nothing changed on it. Forget
		// it, or every later restore would fail on it.
		if !known {
			delete(m.originals, majMin)
			m.forgetState()
		}

		return "", fmt.Errorf("%w (io.cost needs a whole blk-mq disk, such as an NVMe "+
			"or SCSI disk, not a partition or a device-mapper device)", err)
	}

	if err := writeLine(m.cgroupPath, qosFile, majMin, qosFixedRate); err != nil {
		return "", err
	}

	m.log.WithFields(logrus.Fields{
		"device": majMin,
		"model":  modelLine,
	}).Info("Applied the io.cost model")

	return modelLine, nil
}

func (m *manager) Restore(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	for majMin, orig := range m.originals {
		if err := restoreSettings(m.cgroupPath, orig); err != nil {
			errs = append(errs, err)

			continue
		}

		delete(m.originals, majMin)
		m.log.WithField("device", majMin).Info("Restored the io.cost settings")
	}

	if len(m.originals) == 0 {
		m.forgetState()
	}

	return errors.Join(errs...)
}

// saveState writes the original settings to a new state file, then removes
// the previous file. A failure leaves the previous file in place.
func (m *manager) saveState() {
	previous := m.stateFile

	path, err := saveStateFile(m.cacheDir, &State{
		CgroupPath: m.cgroupPath,
		Devices:    deviceList(m.originals),
	})
	if err != nil {
		m.log.WithError(err).Warn("Failed to save the io.cost state file")

		return
	}

	m.stateFile = path

	if previous != "" && previous != path {
		if err := removeStateFile(previous); err != nil {
			m.log.WithError(err).Warn("Failed to remove the previous io.cost state file")
		}
	}
}

// forgetState updates the state file after a device was dropped. Without
// devices, the file is removed.
func (m *manager) forgetState() {
	if len(m.originals) > 0 {
		m.saveState()

		return
	}

	if m.stateFile != "" {
		if err := removeStateFile(m.stateFile); err != nil {
			m.log.WithError(err).Warn("Failed to remove the io.cost state file")
		}

		m.stateFile = ""
	}
}

func deviceList(settings map[string]DeviceSettings) []DeviceSettings {
	list := make([]DeviceSettings, 0, len(settings))
	for _, s := range settings {
		list = append(list, s)
	}

	return list
}

// IsSupported reports if the cgroup hierarchy at cgroupPath has io.cost.
func IsSupported(cgroupPath string) bool {
	_, err := os.Stat(filepath.Join(cgroupPath, modelFile))

	return err == nil
}

// HasWriteAccess checks that the process can change the io.cost settings.
func HasWriteAccess(cgroupPath string) error {
	for _, name := range []string{modelFile, qosFile} {
		f, err := os.OpenFile(filepath.Join(cgroupPath, name), os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("no write access to %s (run as root, with the host cgroup "+
				"namespace in a container): %w", filepath.Join(cgroupPath, name), err)
		}

		_ = f.Close()
	}

	return nil
}

// readSettings returns the io.cost lines of majMin. A device that was never
// configured has no lines.
func readSettings(cgroupPath, majMin string) (DeviceSettings, error) {
	settings := DeviceSettings{MajMin: majMin}

	for name, dst := range map[string]*string{modelFile: &settings.Model, qosFile: &settings.QoS} {
		data, err := os.ReadFile(filepath.Join(cgroupPath, name))
		if err != nil {
			return settings, fmt.Errorf("reading %s: %w", name, err)
		}

		*dst = findLine(string(data), majMin)
	}

	return settings, nil
}

// findLine returns the parameters of the line for majMin, without the
// device number.
func findLine(content, majMin string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if params, ok := strings.CutPrefix(line, majMin+" "); ok {
			return strings.TrimSpace(params)
		}
	}

	return ""
}

// restoreSettings puts back the original lines. A device without lines, or
// with automatic settings, goes back to the kernel defaults.
func restoreSettings(cgroupPath string, orig DeviceSettings) error {
	// Turn io.cost off (or back to its old state) before the model changes.
	qos := "enable=0 ctrl=auto"
	if orig.QoS != "" {
		qos = orig.QoS
		if paramValue(orig.QoS, "ctrl") == "auto" {
			qos = fmt.Sprintf("enable=%s ctrl=auto", orDefault(paramValue(orig.QoS, "enable"), "0"))
		}
	}

	if err := writeLine(cgroupPath, qosFile, orig.MajMin, qos); err != nil {
		return err
	}

	model := "ctrl=auto"
	if orig.Model != "" && paramValue(orig.Model, "ctrl") == "user" {
		model = orig.Model
	}

	return writeLine(cgroupPath, modelFile, orig.MajMin, model)
}

// paramValue returns the value of key in a "key=value key=value" line.
func paramValue(params, key string) string {
	for field := range strings.FieldsSeq(params) {
		if value, ok := strings.CutPrefix(field, key+"="); ok {
			return value
		}
	}

	return ""
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}

	return v
}

func writeLine(cgroupPath, name, majMin, params string) error {
	path := filepath.Join(cgroupPath, name)
	line := majMin + " " + params

	//nolint:gosec // A cgroup control file, not a user path.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	// The kernel parses one write, so the line goes out in one call.
	_, writeErr := f.WriteString(line)
	closeErr := f.Close()

	if writeErr != nil {
		return fmt.Errorf("writing %q to %s: %w", line, path, writeErr)
	}

	if closeErr != nil {
		return fmt.Errorf("closing %s: %w", path, closeErr)
	}

	return nil
}
