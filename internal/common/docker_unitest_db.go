package common

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/gomega"
	"github.com/ory/dockertest/v3"
	dc "github.com/ory/dockertest/v3/docker"
	"k8s.io/apimachinery/pkg/util/wait"
)

// DockerDBContext is a DBContext that runs postgresql as a docker container
type DockerDBContext struct {
	resource      *dockertest.Resource
	pool          *dockertest.Pool
	containerName string
}

func getDockerDBContext(containerName string) (*DockerDBContext, error) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, err
	}

	return &DockerDBContext{
		pool:          pool,
		containerName: containerName,
	}, nil
}

func (c *DockerDBContext) RunDatabase() error {
	newContainer, err := c.pool.RunWithOptions(&dockertest.RunOptions{
		Repository: databaseContainerImage,
		Tag:        databaseContainerImageTag,
		Env:        []string{fmt.Sprintf("POSTGRESQL_ADMIN_PASSWORD=%s", databaseAdminPassword)},
		Name:       c.containerName,
	}, func(hc *dc.HostConfig) {
		hc.Mounts = append(hc.Mounts, dc.HostMount{
			Target:   databaseDataDir,
			Type:     "tmpfs",
			ReadOnly: false,
		})
		hc.AutoRemove = true
	})
	if err != nil {
		return err
	}

	c.resource = newContainer

	defer func() {
		if err != nil {
			if err = c.pool.Purge(c.resource); err != nil {
				fmt.Printf("Failed to purge docker resource: %s", err)
			}
		}
	}()

	host, port := c.GetDatabaseHostPort()
	dbHostPort := fmt.Sprintf("%s:%s", host, port)

	err = wait.PollImmediate(time.Second*3, time.Minute*3, func() (bool, error) {
		// We can't use newContainer.Exec because of https://github.com/ory/dockertest/issues/372
		exec, err2 := c.pool.Client.CreateExec(dc.CreateExecOptions{
			Container: c.resource.Container.ID,
			Cmd:       []string{"pg_isready"},
		})
		if err2 != nil {
			return false, fmt.Errorf("create exec failed: %w", err)
		}

		err2 = c.pool.Client.StartExec(exec.ID, dc.StartExecOptions{})
		if err2 != nil {
			return false, fmt.Errorf("start exec failed: %w", err)
		}

		exitCode := -1
		for {
			inspectExec, err2 := c.pool.Client.InspectExec(exec.ID)
			if err2 != nil {
				return false, fmt.Errorf("inspect exec failed: %w", err)
			}

			if !inspectExec.Running {
				exitCode = inspectExec.ExitCode
				break
			}
		}

		if exitCode != 0 {
			fmt.Printf("Waiting for docker database. pg_isready err: %v, exit code: %d, retrying...\n", err, exitCode)
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("timed out waiting for postgres docker container %s to be ready: %w", dbHostPort, err)
	}

	return nil
}

func (c *DockerDBContext) TeardownDatabase() {
	Expect(c.pool).ShouldNot(BeNil())
	err := c.pool.Purge(c.resource)
	Expect(err).ShouldNot(HaveOccurred())
	c.pool = nil
}

func (c *DockerDBContext) GetDatabaseHostPort() (string, string) {
	host := "127.0.0.1"
	port := strconv.Itoa(databaseDefaultPort)

	if c.resource != nil {
		port = c.resource.GetPort(fmt.Sprintf("%d/tcp", databaseDefaultPort))
	}

	return host, port
}
