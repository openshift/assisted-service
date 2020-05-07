package subsystem

import (
	"context"

	"github.com/filanov/bm-inventory/client/installer"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
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
