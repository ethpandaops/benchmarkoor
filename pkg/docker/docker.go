package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/sirupsen/logrus"
)

// Manager handles Docker operations for benchmarkoor.
type Manager interface {
	Start(ctx context.Context) error
	Stop() error

	// Network operations.
	EnsureNetwork(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error

	// Container operations.
	CreateContainer(ctx context.Context, spec *ContainerSpec) (string, error)
	StartContainer(ctx context.Context, containerID string) error
	StopContainer(ctx context.Context, containerID string) error
	RemoveContainer(ctx context.Context, containerID string) error

	// Init container support.
	RunInitContainer(ctx context.Context, spec *ContainerSpec, stdout, stderr io.Writer) error

	// Log streaming.
	StreamLogs(ctx context.Context, containerID string, stdout, stderr io.Writer) error

	// Image operations.
	PullImage(ctx context.Context, imageName string, policy string) error
	GetImageDigest(ctx context.Context, imageName string) (string, error)

	// Container info.
	GetContainerIP(ctx context.Context, containerID, networkName string) (string, error)

	// Volume operations.
	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	RemoveVolume(ctx context.Context, name string) error

	// Cleanup operations.
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
	ListVolumes(ctx context.Context) ([]VolumeInfo, error)

	// GetClient returns the underlying Docker client for direct API access.
	GetClient() *client.Client

	// WaitForContainerExit returns channels that signal when a container exits.
	// The statusCh receives the exit code, errCh receives any wait errors.
	WaitForContainerExit(ctx context.Context, containerID string) (<-chan int64, <-chan error)
}

// ResourceLimits defines container resource constraints.
type ResourceLimits struct {
	CpusetCpus       string // Comma-separated CPU IDs (e.g., "0,1,2")
	MemoryBytes      int64  // Memory limit in bytes
	MemorySwapBytes  int64  // Memory+swap limit (-1 = unlimited, same as MemoryBytes = no swap)
	MemorySwappiness *int64 // 0-100, controls swappiness
}

// ContainerSpec defines container configuration.
type ContainerSpec struct {
	Name           string
	Image          string
	Entrypoint     []string
	Command        []string
	Env            map[string]string
	Mounts         []Mount
	NetworkName    string
	Labels         map[string]string
	ResourceLimits *ResourceLimits
}

// Mount defines a volume mount.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
	Type     string // "bind", "volume", "tmpfs"
	Content  []byte // For in-memory content to be written to a temp file
}

// ContainerInfo contains information about a container for cleanup.
type ContainerInfo struct {
	ID     string
	Name   string
	Labels map[string]string
}

// VolumeInfo contains information about a volume for cleanup.
type VolumeInfo struct {
	Name   string
	Labels map[string]string
}

// NewManager creates a new Docker manager.
func NewManager(log logrus.FieldLogger) (Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	return &manager{
		log:    log.WithField("component", "docker"),
		client: cli,
		done:   make(chan struct{}),
	}, nil
}

type manager struct {
	log    logrus.FieldLogger
	client *client.Client
	done   chan struct{}
	wg     sync.WaitGroup
}

// Ensure interface compliance.
var _ Manager = (*manager)(nil)

// Start initializes the Docker manager.
func (m *manager) Start(ctx context.Context) error {
	_, err := m.client.Ping(ctx)
	if err != nil {
		return fmt.Errorf("connecting to docker daemon: %w", err)
	}

	m.log.Debug("Connected to Docker daemon")

	return nil
}

// Stop cleans up the Docker manager.
func (m *manager) Stop() error {
	close(m.done)
	m.wg.Wait()

	if err := m.client.Close(); err != nil {
		return fmt.Errorf("closing docker client: %w", err)
	}

	return nil
}

// EnsureNetwork creates a Docker network if it doesn't exist.
func (m *manager) EnsureNetwork(ctx context.Context, name string) error {
	networks, err := m.client.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return fmt.Errorf("listing networks: %w", err)
	}

	for _, net := range networks {
		if net.Name == name {
			m.log.WithField("network", name).Debug("Network already exists")

			return nil
		}
	}

	_, err = m.client.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return fmt.Errorf("creating network %s: %w", name, err)
	}

	m.log.WithField("network", name).Info("Created Docker network")

	return nil
}

// RemoveNetwork removes a Docker network.
func (m *manager) RemoveNetwork(ctx context.Context, name string) error {
	if err := m.client.NetworkRemove(ctx, name); err != nil {
		return fmt.Errorf("removing network %s: %w", name, err)
	}

	m.log.WithField("network", name).Info("Removed Docker network")

	return nil
}

// CreateContainer creates a new container from the spec.
func (m *manager) CreateContainer(ctx context.Context, spec *ContainerSpec) (string, error) {
	log := m.log.WithField("container", spec.Name)

	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	mounts := make([]mount.Mount, 0, len(spec.Mounts))

	for _, mnt := range spec.Mounts {
		mounts = append(mounts, mount.Mount{
			Type:     mount.Type(mnt.Type),
			Source:   mnt.Source,
			Target:   mnt.Target,
			ReadOnly: mnt.ReadOnly,
		})
	}

	containerCfg := &container.Config{
		Image:      spec.Image,
		User:       "root",
		Env:        env,
		Labels:     spec.Labels,
		Entrypoint: spec.Entrypoint,
		Cmd:        spec.Command,
	}

	hostCfg := &container.HostConfig{
		Mounts:      mounts,
		NetworkMode: container.NetworkMode(spec.NetworkName),
	}

	// Apply resource limits if configured.
	if spec.ResourceLimits != nil {
		hostCfg.CpusetCpus = spec.ResourceLimits.CpusetCpus
		hostCfg.Memory = spec.ResourceLimits.MemoryBytes
		hostCfg.MemorySwap = spec.ResourceLimits.MemorySwapBytes
		hostCfg.MemorySwappiness = spec.ResourceLimits.MemorySwappiness
	}

	networkCfg := &network.NetworkingConfig{}

	resp, err := m.client.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}

	log.WithField("id", resp.ID[:12]).Debug("Created container")

	return resp.ID, nil
}

// StartContainer starts a container.
func (m *manager) StartContainer(ctx context.Context, containerID string) error {
	if err := m.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting container %s: %w", containerID[:12], err)
	}

	m.log.WithField("id", containerID[:12]).Debug("Started container")

	return nil
}

// StopContainer stops a container.
func (m *manager) StopContainer(ctx context.Context, containerID string) error {
	if err := m.client.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		return fmt.Errorf("stopping container %s: %w", containerID[:12], err)
	}

	m.log.WithField("id", containerID[:12]).Debug("Stopped container")

	return nil
}

// RemoveContainer removes a container.
func (m *manager) RemoveContainer(ctx context.Context, containerID string) error {
	if err := m.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	}); err != nil {
		return fmt.Errorf("removing container %s: %w", containerID[:12], err)
	}

	m.log.WithField("id", containerID[:12]).Debug("Removed container")

	return nil
}

// RunInitContainer runs an init container and waits for it to complete.
func (m *manager) RunInitContainer(ctx context.Context, spec *ContainerSpec, stdout, stderr io.Writer) error {
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

	statusCh, errCh := m.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

	select {
	case err := <-errCh:
		return fmt.Errorf("waiting for init container: %w", err)
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("init container exited with code %d", status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	log.Debug("Init container completed successfully")

	return nil
}

// StreamLogs streams container logs to the provided writers.
func (m *manager) StreamLogs(ctx context.Context, containerID string, stdout, stderr io.Writer) error {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
	}

	reader, err := m.client.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return fmt.Errorf("getting container logs: %w", err)
	}
	defer func() { _ = reader.Close() }()

	_, err = stdcopy.StdCopy(stdout, stderr, reader)
	if err != nil && err != io.EOF {
		return fmt.Errorf("copying logs: %w", err)
	}

	return nil
}

// PullImage pulls a Docker image.
func (m *manager) PullImage(ctx context.Context, imageName string, policy string) error {
	log := m.log.WithField("image", imageName)

	if policy == "never" {
		log.Debug("Skipping image pull (policy: never)")

		return nil
	}

	if policy == "if-not-present" {
		images, err := m.client.ImageList(ctx, image.ListOptions{
			Filters: filters.NewArgs(filters.Arg("reference", imageName)),
		})
		if err != nil {
			return fmt.Errorf("listing images: %w", err)
		}

		if len(images) > 0 {
			log.Debug("Image already exists (policy: if-not-present)")

			return nil
		}
	}

	log.Info("Pulling image")

	reader, err := m.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}
	defer func() { _ = reader.Close() }()

	// Consume the pull output.
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("reading pull response: %w", err)
	}

	log.Info("Image pulled successfully")

	return nil
}

// GetImageDigest returns the SHA256 digest of an image (just the "sha256:..." portion).
func (m *manager) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	inspect, _, err := m.client.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return "", fmt.Errorf("inspecting image: %w", err)
	}

	// RepoDigests contains "image@sha256:hash" format.
	// Extract just the "sha256:..." portion.
	if len(inspect.RepoDigests) > 0 {
		digest := inspect.RepoDigests[0]
		if idx := strings.Index(digest, "sha256:"); idx != -1 {
			return digest[idx:], nil
		}

		return digest, nil
	}

	// Fallback to image ID (already in sha256:... format).
	return inspect.ID, nil
}

// GetContainerIP returns the IP address of a container in the specified network.
func (m *manager) GetContainerIP(ctx context.Context, containerID, networkName string) (string, error) {
	inspect, err := m.client.ContainerInspect(ctx, containerID)
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

// CreateVolume creates a Docker volume with the given name and labels.
func (m *manager) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	_, err := m.client.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("creating volume %s: %w", name, err)
	}

	m.log.WithField("volume", name).Debug("Created volume")

	return nil
}

// RemoveVolume removes a Docker volume.
func (m *manager) RemoveVolume(ctx context.Context, name string) error {
	if err := m.client.VolumeRemove(ctx, name, true); err != nil {
		return fmt.Errorf("removing volume %s: %w", name, err)
	}

	m.log.WithField("volume", name).Info("Removed volume")

	return nil
}

// ListContainers returns all containers managed by benchmarkoor.
func (m *manager) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	containers, err := m.client.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "benchmarkoor.managed-by=benchmarkoor"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}

		result = append(result, ContainerInfo{
			ID:     c.ID,
			Name:   name,
			Labels: c.Labels,
		})
	}

	return result, nil
}

// ListVolumes returns all volumes managed by benchmarkoor.
func (m *manager) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	volumes, err := m.client.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "benchmarkoor.managed-by=benchmarkoor"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("listing volumes: %w", err)
	}

	result := make([]VolumeInfo, 0, len(volumes.Volumes))
	for _, v := range volumes.Volumes {
		result = append(result, VolumeInfo{
			Name:   v.Name,
			Labels: v.Labels,
		})
	}

	return result, nil
}

// GetClient returns the underlying Docker client for direct API access.
func (m *manager) GetClient() *client.Client {
	return m.client
}

// WaitForContainerExit returns channels that signal when a container exits.
// The statusCh receives the exit code, errCh receives any wait errors.
func (m *manager) WaitForContainerExit(
	ctx context.Context,
	containerID string,
) (<-chan int64, <-chan error) {
	statusCh := make(chan int64, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(statusCh)
		defer close(errCh)

		waitStatusCh, waitErrCh := m.client.ContainerWait(
			ctx, containerID, container.WaitConditionNotRunning,
		)

		select {
		case status := <-waitStatusCh:
			statusCh <- status.StatusCode
		case err := <-waitErrCh:
			errCh <- err
		case <-ctx.Done():
			errCh <- ctx.Err()
		}
	}()

	return statusCh, errCh
}
