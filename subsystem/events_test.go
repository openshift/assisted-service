package subsystem

import (
	"context"
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/events"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
)

var _ = Describe("Events tests", func() {

	AfterEach(func() {
		clearDB()
	})

	Context("query with no filters - expect StatusInternalServerError", func() {
		tests := []struct {
			name    string
			context context.Context
		}{
			{
				name:    "query by user with no filters - expect StatusInternalServerError",
				context: context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
			},
			{
				name:    "query with no user and with no filters - expect StatusInternalServerError",
				context: context.TODO(),
			},
		}

		for i := range tests {
			test := tests[i]
			It(test.name, func() {
				_, err := userBMClient.Events.V2ListEvents(test.context, &events.V2ListEventsParams{
					ClusterID:  nil,
					HostID:     nil,
					InfraEnvID: nil,
					Categories: []string{models.EventCategoryUser},
				})

				Expect(err).ShouldNot(BeNil())
				isExpectedAPIError(err, http.StatusInternalServerError)
			})
		}
	})

	Context("query filtering by cluster_id", func() {
		tests := []struct {
			name                string
			createClusterClient *client.AssistedInstall
			listEventsCtxClient *client.AssistedInstall
			createCluster       bool
			querySucess         bool
			err                 int
		}{
			{
				name:                "filter by user owned cluster - user with user role",
				createClusterClient: userBMClient,
				listEventsCtxClient: userBMClient,
				createCluster:       true,
				querySucess:         true,
				err:                 0,
			},
			{
				name:                "filter by user owned cluster - user with admin role",
				createClusterClient: userBMClient,
				listEventsCtxClient: userBMClient,
				createCluster:       true,
				querySucess:         true,
				err:                 0,
			},
			{
				name:                "filter by user which does not own the cluster - user with user role",
				createClusterClient: userBMClient,
				listEventsCtxClient: user2BMClient,
				createCluster:       true,
				querySucess:         false,
				err:                 http.StatusNotFound,
			},
			{
				name:                "filter by user which does not own the cluster - user with admin role",
				createClusterClient: userBMClient,
				listEventsCtxClient: readOnlyAdminUserBMClient,
				createCluster:       true,
				querySucess:         true,
				err:                 0,
			},
			{
				name:                "filter by an invalid cluster_id - user with user role",
				createClusterClient: userBMClient,
				listEventsCtxClient: userBMClient,
				createCluster:       false,
				querySucess:         false,
				err:                 http.StatusNotFound,
			},
		}
		for i := range tests {
			test := tests[i]
			It(test.name, func() {
				clusterId := strfmt.UUID(uuid.New().String())
				if test.createCluster {
					c, err := test.createClusterClient.Installer.V2RegisterCluster(context.TODO(), &installer.V2RegisterClusterParams{
						NewClusterParams: &models.ClusterCreateParams{
							BaseDNSDomain:     "fake.domain",
							Name:              swag.String("test-v2events-cluster"),
							OpenshiftVersion:  swag.String(openshiftVersion),
							PullSecret:        swag.String(pullSecret),
							VipDhcpAllocation: swag.Bool(false),
						},
					})
					Expect(err).NotTo(HaveOccurred())
					clusterId = *c.GetPayload().ID
				}

				evs, err := test.listEventsCtxClient.Events.V2ListEvents(context.TODO(), &events.V2ListEventsParams{
					ClusterID:  &clusterId,
					HostID:     nil,
					InfraEnvID: nil,
					Categories: []string{models.EventCategoryUser},
				})

				if test.querySucess {
					Expect(err).Should(BeNil())
					Expect(len(evs.GetPayload())).ShouldNot(Equal(0))
					for idx := range evs.GetPayload() {
						Expect(evs.GetPayload()[idx].ClusterID).Should(Equal(&clusterId))
					}
				} else {
					Expect(err).ShouldNot(BeNil())
					isExpectedAPIError(err, test.err)
				}
			})
		}
	})
})

func isExpectedAPIError(err error, httpErrorCode int) bool {
	switch serr := err.(type) {
	case *common.ApiErrorResponse:
		return int(serr.StatusCode()) == httpErrorCode
	case *common.InfraErrorResponse:
		return int(serr.StatusCode()) == httpErrorCode
	default:
		return false
	}
}
