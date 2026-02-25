package podman

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/network"
	"github.com/containers/podman/v5/pkg/bindings/system"
	"github.com/containers/podman/v5/pkg/bindings/volumes"
	entitiesTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	nettypes "go.podman.io/common/libnetwork/types"
)

// DefaultSocket is the default rootful Podman socket path.
const DefaultSocket = "unix:///run/podman/podman.sock"

// qualifyImageName ensures the image name is fully qualified for Podman.
// Docker defaults short names like "ethpandaops/geth:tag" to "docker.io/ethpandaops/geth:tag",
// but Podman requires fully-qualified names unless unqualified-search registries are configured.
func qualifyImageName(name string) string {
	// Already has a registry (contains a dot before the first slash).
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 2 && strings.Contains(parts[0], ".") {
		return name
	}

	return "docker.io/" + name
}

// manager implements docker.ContainerManager using Podman Go bindings.
type manager struct {
	log  logrus.FieldLogger
	conn context.Context // Podman connection context.
	done chan struct{}
	wg   sync.WaitGroup
}

// Ensure interface compliance.
var _ docker.ContainerManager = (*manager)(nil)

// NewManager creates a new Podman container manager.
func NewManager(log logrus.FieldLogger) (docker.ContainerManager, error) {
	return &manager{
		log:  log.WithField("component", "podman"),
		done: make(chan struct{}),
	}, nil
}

// Start initializes the Podman connection and validates the runtime mode.
func (m *manager) Start(ctx context.Context) error {
	conn, err := bindings.NewConnection(ctx, DefaultSocket)
	if err != nil {
		return fmt.Errorf(
			"connecting to podman socket (%s): %w\n"+
				"Ensure the Podman service is running: systemctl start podman.socket",
			DefaultSocket, err,
		)
	}

	m.conn = conn

	// Validate the connection and check runtime mode.
	info, err := system.Info(m.conn, nil)
	if err != nil {
		return fmt.Errorf("querying podman info: %w", err)
	}

	if info.Host.Security.Rootless {
		return fmt.Errorf(
			"podman is running in rootless mode, but benchmarkoor requires rootful podman; " +
				"run the podman service as root or use: sudo systemctl start podman.socket",
		)
	}

	m.log.WithFields(logrus.Fields{
		"version": info.Version.Version,
		"runtime": info.Host.OCIRuntime.Name,
	}).Debug("Connected to Podman daemon")

	return nil
}

// Stop cleans up the Podman manager.
func (m *manager) Stop() error {
	close(m.done)
	m.wg.Wait()

	return nil
}

// EnsureNetwork creates a Podman network if it doesn't exist.
func (m *manager) EnsureNetwork(ctx context.Context, name string) error {
	nets, err := network.List(m.conn, &network.ListOptions{
		Filters: map[string][]string{"name": {name}},
	})
	if err != nil {
		return fmt.Errorf("listing networks: %w", err)
	}

	for _, n := range nets {
		if n.Name == name {
			m.log.WithField("network", name).Debug("Network already exists")

			return nil
		}
	}

	netCfg := nettypes.Network{
		Name:   name,
		Driver: "bridge",
	}

	if _, err := network.Create(m.conn, &netCfg); err != nil {
		return fmt.Errorf("creating network %s: %w", name, err)
	}

	m.log.WithField("network", name).Info("Created Podman network")

	return nil
}

// RemoveNetwork removes a Podman network.
func (m *manager) RemoveNetwork(ctx context.Context, name string) error {
	if _, err := network.Remove(m.conn, name, nil); err != nil {
		return fmt.Errorf("removing network %s: %w", name, err)
	}

	m.log.WithField("network", name).Info("Removed Podman network")

	return nil
}

// CreateContainer creates a new container from the spec using Podman's specgen.
func (m *manager) CreateContainer(
	ctx context.Context, spec *docker.ContainerSpec,
) (string, error) {
	log := m.log.WithField("container", spec.Name)

	s := &specgen.SpecGenerator{}
	s.Name = spec.Name
	s.Image = qualifyImageName(spec.Image)
	s.HealthLogDestination = "local"
	s.Entrypoint = spec.Entrypoint
	s.Command = spec.Command
	s.Labels = spec.Labels
	s.User = "root"
	s.CapAdd = spec.CapAdd

	// Map SecurityOpt entries to specgen fields.
	for _, opt := range spec.SecurityOpt {
		if opt == "seccomp=unconfined" || opt == "seccomp:unconfined" {
			s.SeccompPolicy = "unconfined"
		}
	}

	// Convert env map.
	if len(spec.Env) > 0 {
		s.Env = make(map[string]string, len(spec.Env))
		for k, v := range spec.Env {
			s.Env[k] = v
		}
	}

	// Convert mounts. Docker-style "volume" mounts must be mapped to Podman's
	// NamedVolume type; OCI runtimes (crun/runc) don't recognise "volume" as a
	// mount type and would fail with "No such device".
	if len(spec.Mounts) > 0 {
		s.Mounts = make([]specs.Mount, 0, len(spec.Mounts))

		for _, mnt := range spec.Mounts {
			if mnt.Type == "volume" {
				nv := &specgen.NamedVolume{
					Name: mnt.Source,
					Dest: mnt.Target,
				}

				if mnt.ReadOnly {
					nv.Options = append(nv.Options, "ro")
				}

				s.Volumes = append(s.Volumes, nv)

				continue
			}

			m := specs.Mount{
				Destination: mnt.Target,
				Source:      mnt.Source,
				Type:        mnt.Type,
			}

			if mnt.ReadOnly {
				m.Options = append(m.Options, "ro")
			}

			s.Mounts = append(s.Mounts, m)
		}
	}

	// Configure network.
	if spec.NetworkName != "" {
		s.Networks = map[string]nettypes.PerNetworkOptions{
			spec.NetworkName: {},
		}
	}

	// Apply resource limits.
	if spec.ResourceLimits != nil {
		s.ResourceLimits = &specs.LinuxResources{}

		if spec.ResourceLimits.CpusetCpus != "" {
			s.ResourceLimits.CPU = &specs.LinuxCPU{
				Cpus: spec.ResourceLimits.CpusetCpus,
			}
		}

		if spec.ResourceLimits.MemoryBytes > 0 {
			mem := spec.ResourceLimits.MemoryBytes
			s.ResourceLimits.Memory = &specs.LinuxMemory{
				Limit: &mem,
			}

			if spec.ResourceLimits.MemorySwapBytes != 0 {
				swap := spec.ResourceLimits.MemorySwapBytes
				s.ResourceLimits.Memory.Swap = &swap
			}
		}
	}

	resp, err := containers.CreateWithSpec(m.conn, s, nil)
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}

	log.WithField("id", resp.ID[:12]).Debug("Created container")

	return resp.ID, nil
}

// StartContainer starts a container.
func (m *manager) StartContainer(ctx context.Context, containerID string) error {
	if err := containers.Start(m.conn, containerID, nil); err != nil {
		return fmt.Errorf("starting container %s: %w", containerID[:12], err)
	}

	m.log.WithField("id", containerID[:12]).Debug("Started container")

	return nil
}

// StopContainer stops a container.
func (m *manager) StopContainer(ctx context.Context, containerID string) error {
	if err := containers.Stop(m.conn, containerID, nil); err != nil {
		return fmt.Errorf("stopping container %s: %w", containerID[:12], err)
	}

	m.log.WithField("id", containerID[:12]).Debug("Stopped container")

	return nil
}

// RemoveContainer removes a container.
func (m *manager) RemoveContainer(ctx context.Context, containerID string) error {
	force := true
	vols := true
	timeout := uint(0) // SIGKILL immediately, skip SIGTERM grace period.

	if _, err := containers.Remove(m.conn, containerID, &containers.RemoveOptions{
		Force:   &force,
		Volumes: &vols,
		Timeout: &timeout,
	}); err != nil {
		return fmt.Errorf("removing container %s: %w", containerID[:12], err)
	}

	m.log.WithField("id", containerID[:12]).Debug("Removed container")

	return nil
}

// RunInitContainer runs an init container and waits for it to complete.
func (m *manager) RunInitContainer(
	ctx context.Context,
	spec *docker.ContainerSpec,
	stdout, stderr io.Writer,
) error {
	log := m.log.WithField("init_container", spec.Name)

	containerID, err := m.CreateContainer(ctx, spec)
	if err != nil {
		return fmt.Errorf("creating init container: %w", err)
	}

	defer func() {
		if rmErr := m.RemoveContainer(context.Background(), containerID); rmErr != nil {
			log.WithError(rmErr).Warn("Failed to remove init container")
		}
	}()

	if err := m.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("starting init container: %w", err)
	}

	// Stream logs in background if writers provided.
	if stdout != nil || stderr != nil {
		go func() {
			if streamErr := m.StreamLogs(ctx, containerID, stdout, stderr); streamErr != nil {
				log.WithError(streamErr).Debug("Init container log streaming ended")
			}
		}()
	}

	// Wait for container to exit.
	exitCode, err := containers.Wait(m.conn, containerID, nil)
	if err != nil {
		return fmt.Errorf("waiting for init container: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("init container exited with code %d", exitCode)
	}

	log.Debug("Init container completed successfully")

	return nil
}

// StreamLogs streams container logs to the provided writers.
func (m *manager) StreamLogs(
	ctx context.Context,
	containerID string,
	stdout, stderr io.Writer,
) error {
	// Unlike Docker's ContainerLogs (which waits for a "created" container
	// to start producing output), Podman's Logs API returns immediately
	// with EOF when the container hasn't started yet. Poll until the
	// container is running so we don't silently lose all log output.
	if err := m.waitForRunning(ctx, containerID); err != nil {
		return fmt.Errorf("waiting for container to start: %w", err)
	}

	follow := true
	showStdout := true
	showStderr := true

	// Derive a context from m.conn (carries Podman connection info) that
	// also cancels when the caller's ctx is cancelled. This ensures that
	// follow-mode log streaming terminates when the caller cancels (e.g.,
	// after a container checkpoint).
	logConn, cancel := context.WithCancel(m.conn)
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-logConn.Done():
		}
	}()

	stdoutCh := make(chan string, 100)
	stderrCh := make(chan string, 100)

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		for line := range stdoutCh {
			if stdout != nil {
				_, _ = io.WriteString(stdout, line+"\n")
			}
		}
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		for line := range stderrCh {
			if stderr != nil {
				_, _ = io.WriteString(stderr, line+"\n")
			}
		}
	}()

	err := containers.Logs(logConn, containerID, &containers.LogOptions{
		Follow: &follow,
		Stdout: &showStdout,
		Stderr: &showStderr,
	}, stdoutCh, stderrCh)

	// Podman's Logs does not close the channels on return. Close them so
	// the reader goroutines exit their range loops.
	close(stdoutCh)
	close(stderrCh)

	wg.Wait()

	if err != nil {
		return fmt.Errorf("streaming logs: %w", err)
	}

	return nil
}

// waitForRunning polls container state until it is running or the context is
// cancelled. This is necessary because Podman's Logs API (unlike Docker's)
// returns immediately with EOF for containers in "created" state.
func (m *manager) waitForRunning(ctx context.Context, containerID string) error {
	for {
		inspect, err := containers.Inspect(m.conn, containerID, nil)
		if err != nil {
			return fmt.Errorf("inspecting container: %w", err)
		}

		if inspect.State != nil && inspect.State.Running {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// PullImage pulls a container image.
func (m *manager) PullImage(ctx context.Context, imageName string, policy string) error {
	imageName = qualifyImageName(imageName)
	log := m.log.WithField("image", imageName)

	if policy == "never" {
		log.Debug("Skipping image pull (policy: never)")

		return nil
	}

	if policy == "if-not-present" {
		_, err := images.GetImage(m.conn, imageName, nil)
		if err == nil {
			log.Debug("Image already exists (policy: if-not-present)")

			return nil
		}
	}

	log.Info("Pulling image")

	if _, err := images.Pull(m.conn, imageName, nil); err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}

	log.Info("Image pulled successfully")

	return nil
}

// GetImageDigest returns the SHA256 digest of an image.
func (m *manager) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	imageName = qualifyImageName(imageName)

	inspect, err := images.GetImage(m.conn, imageName, nil)
	if err != nil {
		return "", fmt.Errorf("inspecting image: %w", err)
	}

	// RepoDigests contains "image@sha256:hash" format.
	if len(inspect.RepoDigests) > 0 {
		digest := inspect.RepoDigests[0]
		if idx := strings.Index(digest, "sha256:"); idx != -1 {
			return digest[idx:], nil
		}

		return digest, nil
	}

	// Fallback to image ID.
	return inspect.ID, nil
}

// GetContainerIP returns the IP address of a container in the specified network.
func (m *manager) GetContainerIP(
	ctx context.Context,
	containerID, networkName string,
) (string, error) {
	inspect, err := containers.Inspect(m.conn, containerID, nil)
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}

	if inspect.NetworkSettings == nil || inspect.NetworkSettings.Networks == nil {
		return "", fmt.Errorf("container has no network settings")
	}

	netSettings, ok := inspect.NetworkSettings.Networks[networkName]
	if !ok {
		return "", fmt.Errorf("container not connected to network %s", networkName)
	}

	return netSettings.IPAddress, nil
}

// CreateVolume creates a Podman volume.
func (m *manager) CreateVolume(
	ctx context.Context,
	name string,
	labels map[string]string,
) error {
	_, err := volumes.Create(m.conn, entitiesTypes.VolumeCreateOptions{
		Name:   name,
		Labels: labels,
	}, nil)
	if err != nil {
		return fmt.Errorf("creating volume %s: %w", name, err)
	}

	m.log.WithField("volume", name).Debug("Created volume")

	return nil
}

// RemoveVolume removes a Podman volume.
func (m *manager) RemoveVolume(ctx context.Context, name string) error {
	force := true

	if err := volumes.Remove(m.conn, name, &volumes.RemoveOptions{
		Force: &force,
	}); err != nil {
		return fmt.Errorf("removing volume %s: %w", name, err)
	}

	m.log.WithField("volume", name).Info("Removed volume")

	return nil
}

// ListContainers returns all containers managed by benchmarkoor.
func (m *manager) ListContainers(ctx context.Context) ([]docker.ContainerInfo, error) {
	all := true

	podmanContainers, err := containers.List(m.conn, &containers.ListOptions{
		All: &all,
		Filters: map[string][]string{
			"label": {"benchmarkoor.managed-by=benchmarkoor"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	result := make([]docker.ContainerInfo, 0, len(podmanContainers))

	for _, c := range podmanContainers {
		name := ""

		if len(c.Names) > 0 {
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}

		result = append(result, docker.ContainerInfo{
			ID:     c.ID,
			Name:   name,
			Labels: c.Labels,
		})
	}

	return result, nil
}

// ListVolumes returns all volumes managed by benchmarkoor.
func (m *manager) ListVolumes(ctx context.Context) ([]docker.VolumeInfo, error) {
	podmanVolumes, err := volumes.List(m.conn, &volumes.ListOptions{
		Filters: map[string][]string{
			"label": {"benchmarkoor.managed-by=benchmarkoor"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing volumes: %w", err)
	}

	result := make([]docker.VolumeInfo, 0, len(podmanVolumes))

	for _, v := range podmanVolumes {
		result = append(result, docker.VolumeInfo{
			Name:   v.Name,
			Labels: v.Labels,
		})
	}

	return result, nil
}

// WaitForContainerExit returns channels that signal when a container exits.
func (m *manager) WaitForContainerExit(
	ctx context.Context,
	containerID string,
) (<-chan docker.ContainerExitInfo, <-chan error) {
	statusCh := make(chan docker.ContainerExitInfo, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(statusCh)
		defer close(errCh)

		exitCode, err := containers.Wait(m.conn, containerID, nil)
		if err != nil {
			errCh <- err

			return
		}

		info := docker.ContainerExitInfo{
			ExitCode: int64(exitCode),
		}

		// Inspect to check for OOM kill. The container may already be
		// removed by the runner's cleanup goroutine by the time we get
		// here, so treat "no such container" as a benign race.
		inspect, inspectErr := containers.Inspect(m.conn, containerID, nil)

		if inspectErr != nil {
			if !strings.Contains(inspectErr.Error(), "no such container") {
				m.log.WithError(inspectErr).Warn(
					"Failed to inspect container for OOM status",
				)
			}
		} else if inspect.State != nil {
			info.OOMKilled = inspect.State.OOMKilled
		}

		statusCh <- info
	}()

	return statusCh, errCh
}
