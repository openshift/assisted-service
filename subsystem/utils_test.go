package subsystem

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
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

func updateProgress(hostID strfmt.UUID, clusterID strfmt.UUID, current_step models.HostStage) {
	updateProgressWithInfo(hostID, clusterID, current_step, "")
}

func updateProgressWithInfo(hostID strfmt.UUID, clusterID strfmt.UUID, current_step models.HostStage, info string) {
	ctx := context.Background()

	installProgress := &models.HostProgress{
		CurrentStage: current_step,
		ProgressInfo: info,
	}
	updateReply, err := bmclient.Installer.UpdateHostInstallProgress(ctx, &installer.UpdateHostInstallProgressParams{
		ClusterID:    clusterID,
		HostProgress: installProgress,
		HostID:       hostID,
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
}
