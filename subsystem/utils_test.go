package subsystem

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/client/installer"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

const (
	defaultWaitForHostStateTimeout    = 20 * time.Second
	defaultWaitForClusterStateTimeout = 30 * time.Second
)

func clearDB() {
	db.Delete(&models.Host{})
	db.Delete(&models.Cluster{})
}

func strToUUID(s string) *strfmt.UUID {
	u := strfmt.UUID(s)
	return &u
}

func registerHost(clusterID strfmt.UUID) *models.Host {
	host, err := bmclient.Installer.RegisterHost(context.Background(), &installer.RegisterHostParams{
		ClusterID: clusterID,
		NewHostParams: &models.HostCreateParams{
			HostID: strToUUID(uuid.New().String()),
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func getHost(clusterID, hostID strfmt.UUID) *models.Host {
	host, err := bmclient.Installer.GetHost(context.Background(), &installer.GetHostParams{
		ClusterID: clusterID,
		HostID:    hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func getStepInList(steps models.Steps, sType models.StepType) (*models.Step, bool) {
	for _, step := range steps.Instructions {
		if step.StepType == sType {
			return step, true
		}
	}
	return nil, false
}

func getNextSteps(clusterID, hostID strfmt.UUID) models.Steps {
	steps, err := bmclient.Installer.GetNextSteps(context.Background(), &installer.GetNextStepsParams{
		ClusterID: clusterID,
		HostID:    hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return *steps.GetPayload()
}
