package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// AutoSpinGarage checks if Garage needs to be started, pulls/starts it, and auto-configures the static credentials.
func AutoSpinGarage(ctx context.Context, endpoint, accessKey, secretKey string) error {
	// Only target localhost endpoints (e.g. http://localhost:3900 or 127.0.0.1:3900)
	if !strings.Contains(endpoint, "localhost:") && !strings.Contains(endpoint, "127.0.0.1:") {
		return nil // Not a local address, skip local container management
	}

	// 1. Establish connection to Docker daemon
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to connect to Docker daemon: %w. If you are in a serverless environment, please point to a remote S3/GCS bucket instead", err)
	}
	defer cli.Close()

	// Ping the Docker daemon to check if it's actually running and accessible
	if _, err := cli.Ping(ctx); err != nil {
		return fmt.Errorf("Docker daemon is not running or accessible: %w. Cannot spin up local Garage container", err)
	}

	// Check if container already exists and is running
	const containerName = "buckstream-local-s3"
	inspect, err := cli.ContainerInspect(ctx, containerName)
	if err == nil {
		if inspect.State.Running {
			log.Println("🐳 Garage storage container is already running. Ensuring credentials are up to date...")
			return configureGarage(ctx, cli, inspect.ID, accessKey, secretKey)
		}
		// If exists but stopped, start it
		log.Println("🐳 Starting existing Garage storage container...")
		if err := cli.ContainerStart(ctx, inspect.ID, container.StartOptions{}); err != nil {
			return fmt.Errorf("failed to start stopped Garage container: %w", err)
		}
		return configureGarage(ctx, cli, inspect.ID, accessKey, secretKey)
	}

	log.Println("🐳 Preparing local Garage storage container...")

	// 2. Ensure local persistence directory exists
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(wd, "tmp", "garage_data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create local data folder: %w", err)
	}

	// Ensure the config file exists
	configPath := filepath.Join(wd, "deploy", "garage", "garage.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("garage.toml config file not found at %s", configPath)
	}

	// 3. Pull Garage image if not present locally
	imageName := "dxflrs/garage:v0.9.0"
	log.Printf("🐳 Pulling image %s (this may take a minute on first run)...", imageName)
	pullReader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull Garage container image: %w", err)
	}
	defer pullReader.Close()

	// Read and discard image pull stream until it reaches EOF (blocking until download completes)
	_, _ = io.Copy(io.Discard, pullReader)

	// 4. Create Container
	containerConfig := &container.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			"3900/tcp": struct{}{},
			"3903/tcp": struct{}{},
		},
		Cmd: []string{"/garage", "-c", "/etc/garage.toml", "server"},
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"3900/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "3900"}},
			"3903/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "3903"}},
		},
		Binds: []string{
			configPath + ":/etc/garage.toml",
			dataDir + ":/data",
		},
	}

	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return fmt.Errorf("failed to create Garage container: %w", err)
	}

	log.Println("🐳 Starting Garage S3 server container...")
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start Garage container: %w", err)
	}

	return configureGarage(ctx, cli, resp.ID, accessKey, secretKey)
}

// configureGarage waits for the container's admin API and ensures the S3 key and bucket exist and are associated.
func configureGarage(ctx context.Context, cli *client.Client, containerID, accessKey, secretKey string) error {
	log.Println("🐳 Configuring static S3 credentials and bucket inside the container...")

	// 1. Wait for API to respond on port 3903
	for i := 0; i < 15; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:3903", 1*time.Second)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(1 * time.Second)
	}

	// 2. Fetch the node ID from status
	nodeID, err := getGarageNodeID(ctx, cli, containerID)
	if err != nil {
		log.Printf("⚠️ Warning: could not retrieve Garage Node ID: %v. Proceeding...", err)
	} else {
		// 3. Stage the single-node layout (assign to local zone with 10G capacity)
		log.Printf("🐳 Staging cluster layout for node %s...", nodeID)
		_, _ = runExec(ctx, cli, containerID, []string{"/garage", "layout", "assign", nodeID, "-c", "10G", "-z", "local"})

		// 4. Apply the layout change
		log.Println("🐳 Enacting cluster layout...")
		_, _ = runExec(ctx, cli, containerID, []string{"/garage", "layout", "apply", "--version", "1"})
	}

	// 5. Run idempotent commands inside the container to configure S3 API key and Bucket

	// Command A: Import the S3 access/secret key pair
	_, _ = runExec(ctx, cli, containerID, []string{"/garage", "key", "import", "--yes", "-n", "buckstream-key", accessKey, secretKey})

	// Command B: Create the S3 bucket 'buckstream'
	_, _ = runExec(ctx, cli, containerID, []string{"/garage", "bucket", "create", "buckstream"})

	// Command C: Allow the S3 key to read/write the bucket
	_, _ = runExec(ctx, cli, containerID, []string{"/garage", "bucket", "allow", "buckstream", "--key", "buckstream-key", "--read", "--write"})

	log.Println("🐳 S3 local Garage container initialized successfully!")
	return nil
}

// getGarageNodeID parses the node ID from the garage status output.
func getGarageNodeID(ctx context.Context, cli *client.Client, containerID string) (string, error) {
	out, err := runExec(ctx, cli, containerID, []string{"/garage", "status"})
	if err != nil {
		return "", err
	}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "ID") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			// First column is the hexadecimal node ID
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("node ID not found in status output")
}

// runExec runs a command inside a running Docker container
func runExec(ctx context.Context, cli *client.Client, containerID string, cmd []string) (string, error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	resp, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", err
	}

	respAttach, err := cli.ContainerExecAttach(ctx, resp.ID, container.ExecStartOptions{})
	if err != nil {
		return "", err
	}
	defer respAttach.Close()

	var outBuf bytes.Buffer
	_, err = outBuf.ReadFrom(respAttach.Reader)
	return outBuf.String(), err
}
