package common

import (
	"context"
	"fmt"
	"log"

	"github.com/docker/go-connections/nat"
	"github.com/pkg/errors"
	testcontainers "github.com/testcontainers/testcontainers-go"
)

type TestContainersDBContext struct {
	ctx         context.Context
	dbContainer testcontainers.Container
}

func (c *TestContainersDBContext) Create() error {
	c.ctx = context.Background()
	var err error
	c.dbContainer, err = testcontainers.GenericContainer(c.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "quay.io/sclorg/postgresql-12-c8s:latest",
			Env:          map[string]string{"POSTGRESQL_ADMIN_PASSWORD": "admin"},
			ExposedPorts: []string{fmt.Sprintf("%s/tcp", dbDefaultPort)},
			Name:         dbDockerName,
		},
		Started: true,
	})
	if err != nil {
		return errors.Wrapf(err, "unable to start db test container")
	}
	return nil
}

func (c *TestContainersDBContext) Teardown() {
	if err := c.dbContainer.Terminate(c.ctx); err != nil {
		log.Fatalf("unable to stop db test container: %s", err)
	}
}

func (c *TestContainersDBContext) GetHostPort() (string, string) {
	host := "127.0.0.1"
	port, err := nat.NewPort("tcp", dbDefaultPort)
	if err != nil {
		log.Fatalf("unable to create port for %s due to error %s", dbDefaultPort, err)
	}
	var mappedPort nat.Port
	if c.dbContainer != nil {
		mappedPort, err = c.dbContainer.MappedPort(c.ctx, port)
		if err != nil {
			log.Fatalf("unable to determine mapped port for %s/tcp: %s", dbDefaultPort, err)
			return "", ""
		}
	}
	return host, mappedPort.Port()
}
