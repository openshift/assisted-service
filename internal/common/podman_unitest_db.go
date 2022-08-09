package common

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// PodmanDBContext is a DBContext that runs postgresql as a podman container
type PodmanDBContext struct {
	containerName string
	port          int
}

func getPodmanDBContext(containerName string) (*PodmanDBContext, error) {
	return &PodmanDBContext{
		containerName: containerName,
	}, nil
}

func runCommand(command string, args ...string) error {
	if output, err := exec.Command("podman", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run command %s with args %v: %w\n%s", command, args, err, output)
	}

	return nil
}

func (p *PodmanDBContext) runPodmanContainer() error {
	args := []string{
		"run",
		"--detach",
		"--rm",
		"--tmpfs", databaseDataDir,
		"--name", p.containerName,
		"--publish-all",
		"--env", fmt.Sprintf("POSTGRESQL_ADMIN_PASSWORD=%s", databaseAdminPassword),
		"--env", "POSTGRESQL_MAX_CONNECTIONS=10000",
		databaseContainerImage,
	}

	if err := runCommand("podman", args...); err != nil {
		return fmt.Errorf("failed to run podman: %w", err)
	}

	return nil
}

func (p *PodmanDBContext) getPublishedPort() (int, error) {
	args := []string{
		"inspect",
		p.containerName,
		databaseContainerImage,
		"--format",
		fmt.Sprintf(`{{(index (index .NetworkSettings.Ports "%d/tcp") 0).HostPort}}`, databaseDefaultPort),
	}

	out, err := exec.Command("podman", args...).Output()
	if err != nil {
		return 0, fmt.Errorf("failed to run podman: %w", err)
	}

	parsedPort, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse podman postgres exposed port: %w", err)
	}

	return parsedPort, nil
}

func (p *PodmanDBContext) stopPodmanContainer() {
	args := []string{"kill", p.containerName}

	if err := runCommand("podman", args...); err != nil {
		fmt.Printf("Failed to kill podman container: %s", err)
	}

}

func (p *PodmanDBContext) checkPostgresHealth() int {
	args := []string{"exec", p.containerName, "pg_isready"}

	if err := exec.Command("podman", args...).Run(); err != nil {
		if err, ok := err.(*exec.ExitError); ok {
			return err.ExitCode()
		} else {
			fmt.Printf("Failed to check postgres podman container health: %s", err)
			return -1
		}
	}

	return 0
}

func (p *PodmanDBContext) RunDatabase() error {
	if err := p.runPodmanContainer(); err != nil {
		return err
	}

	var err error

	defer func() {
		if err != nil {
			p.stopPodmanContainer()
		}
	}()

	err = wait.PollImmediate(time.Millisecond*100, time.Minute*3, func() (bool, error) {
		exitCode := p.checkPostgresHealth()
		if exitCode != 0 {
			fmt.Printf("Waiting for podman database. pg_isready err: %v, exit code: %d, retrying...\n", err, exitCode)
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("timed out waiting for postgres podman container %s to be ready: %w", p.containerName, err)
	}

	port, err := p.getPublishedPort()
	if err != nil {
		return fmt.Errorf("failed to get published port for podman database: %s", err)
	}

	p.port = port

	return nil
}

func (p *PodmanDBContext) TeardownDatabase() {
	p.stopPodmanContainer()
}

func (p *PodmanDBContext) GetDatabaseHostPort() (string, string) {
	if p.port == 0 {
		panic("database not running")
	}

	host := "127.0.0.1"

	return host, strconv.Itoa(p.port)
}
