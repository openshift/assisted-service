package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang-jwt/jwt/v4"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/events"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/managed_domains"
	"github.com/openshift/assisted-service/client/versions"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	params "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

var _ = Describe("NewAuthzHandler", func() {
	It("Is disabled unless auth type is rhsso", func() {
		cfg := &Config{AuthType: TypeRHSSO}
		handler := NewAuthzHandler(cfg, nil, logrus.New(), nil)
		_, ok := handler.(*AuthzHandler)
		Expect(ok).To(BeTrue())

		cfg = &Config{}
		handler = NewAuthzHandler(cfg, nil, logrus.New(), nil)
		_, ok = handler.(*NoneHandler)
		Expect(ok).To(BeTrue())

		cfg = &Config{AuthType: TypeNone}
		handler = NewAuthzHandler(cfg, nil, logrus.New(), nil)
		_, ok = handler.(*NoneHandler)
		Expect(ok).To(BeTrue())
	})
})

var _ = Describe("Authz email domain", func() {
	tests := []struct {
		name           string
		email          string
		expectedDomain string
	}{
		{
			name:           "Valid email",
			email:          "admin@example.com",
			expectedDomain: "example.com",
		},
		{
			name:           "Valid email with special case, @ in local part",
			email:          "\"@\"dmin@example.com",
			expectedDomain: "example.com",
		},
		{
			name:           "Invalid email address",
			email:          "foobar",
			expectedDomain: ocm.UnknownEmailDomain,
		},
		{
			name:           "Empty value",
			email:          "",
			expectedDomain: ocm.UnknownEmailDomain,
		},
	}

	for _, tt := range tests {
		tt := tt
		It(fmt.Sprintf("test %s", tt.name), func() {
			payload := &ocm.AuthPayload{}
			payload.Email = tt.email
			ctx := context.Background()
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			domain := ocm.EmailDomainFromContext(ctx)
			Expect(domain).To(Equal(tt.expectedDomain))
		})
	}
})

var _ = Describe("OwnedBy", func() {
	var (
		db      *gorm.DB
		dbName  string
		ctx     context.Context
		handler Authorizer
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctx = context.Background()

		//prepare test data
		db.Model(&common.Cluster{}).Create([]map[string]interface{}{
			//Organization 1
			{"ID": strfmt.UUID(uuid.New().String()), "Name": "A", "UserName": "user1", "OrgID": "org1"},
			{"ID": strfmt.UUID(uuid.New().String()), "Name": "B", "UserName": "user2", "OrgID": "org1"},
			//Organization 2
			{"ID": strfmt.UUID(uuid.New().String()), "Name": "A", "UserName": "user1", "OrgID": "org2"},
			//No user data
			{"ID": strfmt.UUID(uuid.New().String()), "Name": "A"},
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("authz RHSSO ownership", func() {
		AllRecordsHasUserName := func(records []common.Cluster, username string) bool {
			return len(funk.Filter(records, func(item common.Cluster) bool {
				return item.UserName == username
			}).([]common.Cluster)) == len(records)
		}

		AllRecordsHasName := func(records []common.Cluster, name string) bool {
			return len(funk.Filter(records, func(item common.Cluster) bool {
				return item.Name == name
			}).([]common.Cluster)) == len(records)
		}

		AllRecordsHasOrgId := func(records []common.Cluster, orgId string) bool {
			return len(funk.Filter(records, func(item common.Cluster) bool {
				return item.OrgID == orgId
			}).([]common.Cluster)) == len(records)
		}

		Context("tenancy disabled", func() {

			BeforeEach(func() {
				cfg := &Config{AuthType: TypeRHSSO, EnableOrgTenancy: false}
				handler = NewAuthzHandler(cfg, nil, logrus.New(), db)
			})

			It("admin user - no conditions", func() {
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.AdminRole
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Find(&records)
				Expect(results.RowsAffected, 4)
			})
			It("admin user - non-empty query", func() {
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.AdminRole
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Where("name = ?", "A").Find(&records)
				Expect(results.RowsAffected, 3)
				Expect(AllRecordsHasName(records, "A")).To(BeTrue())
			})
			It("admin user - ownerByUser filters by user name", func() {
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.AdminRole
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedByUser(ctx, db, "user1").Find(&records)
				Expect(results.RowsAffected, 2)
				Expect(AllRecordsHasUserName(records, "user1")).To(BeTrue())
			})

			It("non-admin user - empty query should filter by user", func() {
				payload := &ocm.AuthPayload{}
				payload.Username = "user1"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Find(&records)
				Expect(results.RowsAffected, 2)
				Expect(AllRecordsHasUserName(records, "user1")).To(BeTrue())
			})
			It("non-admin user - non-empty query should filter by user", func() {
				payload := &ocm.AuthPayload{}
				payload.Username = "user1"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Where("name = ?", "A").Find(&records)
				Expect(results.RowsAffected, 2)
				Expect(AllRecordsHasName(records, "A")).To(BeTrue())
				Expect(AllRecordsHasUserName(records, "user1")).To(BeTrue())
			})
			It("non-admin user - ownedByUser filters by user if equals current user", func() {
				payload := &ocm.AuthPayload{}
				payload.Username = "user1"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedByUser(ctx, db, "user1").Find(&records)
				Expect(results.RowsAffected, 2)
				Expect(AllRecordsHasUserName(records, "user1")).To(BeTrue())
			})
			It("non-admin user - ownedByUser should block if not equals current user", func() {
				payload := &ocm.AuthPayload{}
				payload.Username = "user1"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedByUser(ctx, db, "user2").Find(&records)
				Expect(results.RowsAffected, 0)
			})
		})
		Context("tenancy enabled", func() {
			BeforeEach(func() {
				cfg := &Config{AuthType: TypeRHSSO, EnableOrgTenancy: true}
				handler = NewAuthzHandler(cfg, nil, logrus.New(), db)
			})
			It("admin user - empty query", func() {
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.AdminRole
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Find(&records)
				Expect(results.RowsAffected, 4)
			})
			It("admin user - non-empty query", func() {
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.AdminRole
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Where("name = ?", "A").Find(&records)
				Expect(results.RowsAffected, 3)
				Expect(AllRecordsHasName(records, "A")).To(BeTrue())
			})
			It("non-admin user - empty query should filter by org", func() {
				payload := &ocm.AuthPayload{}
				payload.Organization = "org1"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Find(&records)
				Expect(results.RowsAffected, 2)
				Expect(AllRecordsHasOrgId(records, "org1")).To(BeTrue())
			})
			It("non-admin user - non-empty query should filter by org", func() {
				payload := &ocm.AuthPayload{}
				payload.Organization = "org1"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedBy(ctx, db).Where("name = ?", "A").Find(&records)
				Expect(results.RowsAffected, 1)
				Expect(AllRecordsHasName(records, "A")).To(BeTrue())
				Expect(AllRecordsHasOrgId(records, "org1")).To(BeTrue())
			})
			It("non-admin user - ownedByUser filters by user and org", func() {
				payload := &ocm.AuthPayload{}
				payload.Username = "user2"
				payload.Organization = "org1"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedByUser(ctx, db, "user1").Find(&records)
				Expect(results.RowsAffected, 1)
				Expect(AllRecordsHasUserName(records, "user1")).To(BeTrue())
			})
			It("non-admin user - ownedByUser should block if user does not exist in the org", func() {
				payload := &ocm.AuthPayload{}
				payload.Username = "user1"
				payload.Organization = "org2"
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)

				var records []common.Cluster
				results := handler.OwnedByUser(ctx, db, "user2").Find(&records)
				Expect(results.RowsAffected, 0)
			})
		})
	})
})

var _ = Describe("HasAccessTo", func() {
	var (
		ctx                    context.Context
		db                     *gorm.DB
		dbName                 string
		ctrl                   *gomock.Controller
		mockOcmAuthorization   *ocm.MockOCMAuthorization
		authzHandler           *AuthzHandler
		id1, id2, subscription strfmt.UUID
	)

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		id1 = strfmt.UUID(uuid.New().String())
		id2 = strfmt.UUID(uuid.New().String())
		subscription = strfmt.UUID(uuid.New().String())

		//prepare test data
		db.Model(&common.Cluster{}).Create([]map[string]interface{}{
			//Organization 1
			{"ID": id1, "Name": "A", "UserName": "user1", "OrgID": "org1", "AmsSubscriptionID": subscription},
			//Organization 2
			{"ID": id2, "Name": "B", "UserName": "user1", "OrgID": "org2", "AmsSubscriptionID": "other"},
		})
		db.Model(&common.InfraEnv{}).Create([]map[string]interface{}{
			//bound infraEnv
			{"ID": id1, "Name": "A", "UserName": "user1", "OrgID": "org1", "ClusterID": id1},
			//Unbound InfraEnv
			{"ID": id2, "Name": "B", "UserName": "user1", "OrgID": "org1"},
		})
		db.Model(&common.Host{}).Create([]map[string]interface{}{
			//no user with bound infraenv (not an actual case.
			//In real life the user will be the same as the infra-env's)
			{"ID": id1, "InfraEnvID": id2},
			//no user with bound cluster
			{"ID": id2, "InfraEnvID": id1, "ClusterID": id2},
		})
	})

	BeforeEach(func() {
		ctx = context.TODO()
		ctrl = gomock.NewController(GinkgoT())
		cfg := &Config{AuthType: TypeRHSSO, EnableOrgTenancy: true}
		mockOcmAuthorization = ocm.NewMockOCMAuthorization(ctrl)
		ocmClient := &ocm.Client{
			Authentication: ocm.NewMockOCMAuthentication(ctrl),
			Authorization:  mockOcmAuthorization,
			Cache:          cache.New(1*time.Minute, 30*time.Minute),
		}

		authzHandler = &AuthzHandler{
			cfg:    cfg,
			log:    logrus.New(),
			db:     db,
			client: ocmClient,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("cluster", func() {
		It("owner can read and write", func() {
			payload := &ocm.AuthPayload{}
			payload.Username = "user1"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			cluster, _ := common.GetClusterFromDB(db, id1, common.SkipEagerLoading)
			Expect(authzHandler.HasAccessTo(ctx, cluster, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, DeleteAction)).To(BeTrue())
		})

		It("ClusterEditorRole can read and write", func() {
			payload := &ocm.AuthPayload{Username: "user2", Organization: "org1"}
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockOcmAuthorization.EXPECT().AccessReview(
				gomock.Any(), "user2", gomock.Any(), subscription.String(), gomock.Any()).
				Return(true, nil).Times(2)
			cluster, _ := common.GetClusterFromDB(db, id1, common.SkipEagerLoading)
			Expect(authzHandler.HasAccessTo(ctx, cluster, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, DeleteAction)).To(BeTrue())
		})

		It("others can read but not write", func() {
			payload := &ocm.AuthPayload{Username: "user2", Organization: "org1"}
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockOcmAuthorization.EXPECT().AccessReview(
				gomock.Any(), "user2", gomock.Any(), subscription.String(), gomock.Any()).
				Return(false, nil).Times(2)
			cluster, _ := common.GetClusterFromDB(db, id1, common.SkipEagerLoading)
			Expect(authzHandler.HasAccessTo(ctx, cluster, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, UpdateAction)).To(BeFalse())
			Expect(authzHandler.HasAccessTo(ctx, cluster, DeleteAction)).To(BeFalse())
		})
		It("admin can always read and write", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			cluster, _ := common.GetClusterFromDB(db, id1, common.SkipEagerLoading)
			Expect(authzHandler.HasAccessTo(ctx, cluster, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, DeleteAction)).To(BeTrue())

			cluster, _ = common.GetClusterFromDB(db, id2, common.SkipEagerLoading)
			Expect(authzHandler.HasAccessTo(ctx, cluster, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, DeleteAction)).To(BeTrue())
		})
		It("read-only-admin can only read clusters", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.ReadOnlyAdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockOcmAuthorization.EXPECT().AccessReview(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(false, nil).Times(4) //delete action is funneled to an update check

			cluster, _ := common.GetClusterFromDB(db, id1, common.SkipEagerLoading)
			Expect(authzHandler.HasAccessTo(ctx, cluster, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, UpdateAction)).To(BeFalse())
			Expect(authzHandler.HasAccessTo(ctx, cluster, DeleteAction)).To(BeFalse())

			cluster, _ = common.GetClusterFromDB(db, id2, common.SkipEagerLoading)
			Expect(authzHandler.HasAccessTo(ctx, cluster, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, cluster, UpdateAction)).To(BeFalse())
			Expect(authzHandler.HasAccessTo(ctx, cluster, DeleteAction)).To(BeFalse())
		})
	})

	Context("infra-env", func() {
		It("owner can read and write", func() {
			payload := &ocm.AuthPayload{}
			payload.Username = "user1"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			infraEnv, _ := common.GetInfraEnvFromDB(db, id1)
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, DeleteAction)).To(BeTrue())
		})

		It("ClusterEditorRole can read and write", func() {
			By("bound infra-env rules according to cluster")
			payload := &ocm.AuthPayload{Username: "user2", Organization: "org1"}
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockOcmAuthorization.EXPECT().AccessReview(
				gomock.Any(), "user2", gomock.Any(), subscription.String(), gomock.Any()).
				Return(true, nil).Times(2)
			infraEnv, _ := common.GetInfraEnvFromDB(db, id1)
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, DeleteAction)).To(BeTrue())

			By("unbound infra-env rules according to infra-env")
			infraEnv, _ = common.GetInfraEnvFromDB(db, id2)
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, ReadAction)).To(BeTrue()) //user2 has access rights because of the org
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, UpdateAction)).To(BeFalse())
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, DeleteAction)).To(BeFalse())
		})

		It("others can read but not write", func() {
			payload := &ocm.AuthPayload{Username: "user2", Organization: "org1"}
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockOcmAuthorization.EXPECT().AccessReview(
				gomock.Any(), "user2", gomock.Any(), subscription.String(), gomock.Any()).
				Return(false, nil).Times(2)
			infraEnv, _ := common.GetInfraEnvFromDB(db, id1)
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, ReadAction)).To(BeTrue()) //org based access
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, UpdateAction)).To(BeFalse())
			Expect(authzHandler.HasAccessTo(ctx, infraEnv, DeleteAction)).To(BeFalse()) //delete is mapped to update
		})
	})

	Context("host", func() {
		It("owner can read and write according to infra-env ownership", func() {
			payload := &ocm.AuthPayload{}
			payload.Username = "user1"
			payload.Organization = "org1"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			host, _ := common.GetHostFromDBbyHostId(db, id1)
			Expect(authzHandler.HasAccessTo(ctx, host, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, host, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, host, DeleteAction)).To(BeTrue())
		})

		It("ClusterEditorRole on cluster-bound host can read and write according to cluster rules", func() {
			payload := &ocm.AuthPayload{Username: "user2", Organization: "org1"}
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			//a non realistic example where a user has no access to the cluster record
			//because they are in another org in this case but access review API grants
			//it an editor role.
			host, _ := common.GetHostFromDBbyHostId(db, id2)
			mockOcmAuthorization.EXPECT().AccessReview(
				gomock.Any(), "user2", gomock.Any(), "other", gomock.Any()).
				Return(true, nil).Times(2) //delete is mapped to update
			Expect(authzHandler.HasAccessTo(ctx, host, ReadAction)).To(BeFalse())
			Expect(authzHandler.HasAccessTo(ctx, host, UpdateAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, host, DeleteAction)).To(BeTrue())
		})

		It("ClusterEditorRole on infra-env can read and write according to infra-env rules", func() {
			payload := &ocm.AuthPayload{Username: "user2", Organization: "org1"}
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockOcmAuthorization.EXPECT().AccessReview(
				gomock.Any(), "user2", gomock.Any(), subscription.String(), gomock.Any()).
				Return(true, nil).Times(0)

			//host is pointing to a non bound infra-env. In this case the access
			//is determined according to the ownership of the infra-env record in the db
			host, _ := common.GetHostFromDBbyHostId(db, id1)
			Expect(authzHandler.HasAccessTo(ctx, host, ReadAction)).To(BeTrue())
			Expect(authzHandler.HasAccessTo(ctx, host, UpdateAction)).To(BeFalse())
			Expect(authzHandler.HasAccessTo(ctx, host, DeleteAction)).To(BeFalse())
		})
	})

	It("non supported object should fail", func() {
		payload := &ocm.AuthPayload{Username: "user2", Organization: "org1"}
		ctx = context.WithValue(ctx, restapi.AuthKey, payload)

		_, err := authzHandler.HasAccessTo(ctx, &models.Event{}, ReadAction)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("IsAdmin", func() {
	var (
		ctx     context.Context
		handler Authorizer
		payload *ocm.AuthPayload
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg := &Config{AuthType: TypeRHSSO}
		handler = NewAuthzHandler(cfg, nil, logrus.New(), nil)
		payload = &ocm.AuthPayload{}
	})

	It("admin user", func() {
		payload.Role = ocm.AdminRole
		ctx = context.WithValue(ctx, restapi.AuthKey, payload)
		isAdmin := handler.IsAdmin(ctx)

		Expect(isAdmin).Should(Equal(true))
	})
	It("readonly admin user", func() {
		payload.Role = ocm.ReadOnlyAdminRole
		ctx = context.WithValue(ctx, restapi.AuthKey, payload)
		isAdmin := handler.IsAdmin(ctx)

		Expect(isAdmin).Should(Equal(true))
	})
	It("non-admin user", func() {
		ctx = context.WithValue(ctx, restapi.AuthKey, payload)
		isAdmin := handler.IsAdmin(ctx)

		Expect(isAdmin).Should(Equal(false))
	})
})

var _ = Describe("authz", func() {
	var (
		server      *httptest.Server
		userClient  *client.AssistedInstall
		agentClient *client.AssistedInstall
		ctx         = context.TODO()
		log         = logrus.New()
		t           = GinkgoT()
		authzCache  = cache.New(time.Hour, 30*time.Minute)
	)

	log.SetOutput(io.Discard)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockOcmAuthz := ocm.NewMockOCMAuthorization(ctrl)
	var adminUsers []string

	passAccessReview := func(times int) {
		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any()).Return(true, nil).Times(times)
	}
	failAccessReview := func(times int) {
		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any()).Return(false, nil).Times(times)
	}

	passCapabilityReview := func(times int) {
		mockOcmAuthz.EXPECT().CapabilityReview(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any()).Return(true, nil).Times(times)
	}

	failCapabilityReview := func(times int) {
		mockOcmAuthz.EXPECT().CapabilityReview(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any()).Return(false, nil).Times(times)
	}

	mockUserAuth := func(token string) (interface{}, error) {
		payload := &ocm.AuthPayload{}
		payload.Username = "test@user"
		payload.FirstName = "jon"
		payload.LastName = "doe"
		payload.Email = "test@user"
		if funk.Contains(adminUsers, "test@user") {
			payload.Role = ocm.AdminRole
			return payload, nil
		}
		isReadOnlyAdmin, err := mockOcmAuthz.CapabilityReview(
			ctx,
			"",
			"",
			"")
		if err != nil {
			return nil, err
		}
		if isReadOnlyAdmin {
			payload.Role = ocm.ReadOnlyAdminRole
		} else {
			payload.Role = ocm.UserRole
		}
		return payload, nil
	}

	mockAgentAuth := func(token string) (interface{}, error) {
		payload := &ocm.AuthPayload{}
		payload.Username = ""
		payload.FirstName = ""
		payload.LastName = ""
		payload.Email = ""
		payload.Role = ocm.UserRole
		return payload, nil
	}

	userToken, JwkCert := GetTokenAndCert(false)
	h, err := restapi.Handler(
		restapi.Config{
			AuthAgentAuth: mockAgentAuth,
			AuthUserAuth:  mockUserAuth,
			Authorizer: NewAuthzHandler(
				&Config{
					AuthType:   TypeRHSSO,
					JwkCertURL: "",
					JwkCert:    string(JwkCert),
				},
				&ocm.Client{
					Authorization: mockOcmAuthz,
					Cache:         authzCache,
				},
				log.WithField("pkg", "auth"), nil).CreateAuthorizer(),
			InstallerAPI:      fakeInventory{},
			EventsAPI:         &fakeEventsAPI{},
			Logger:            logrus.Printf,
			VersionsAPI:       fakeVersionsAPI{},
			ManagedDomainsAPI: fakeManagedDomainsAPI{},
			InnerMiddleware:   nil,
		})
	Expect(err).To(BeNil())

	BeforeEach(func() {
		server = httptest.NewServer(h)

		srvUrl := &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   strings.TrimPrefix(server.URL, "http://"),
			Path:   client.DefaultBasePath,
		}
		userClient = client.New(
			client.Config{
				URL:      srvUrl,
				AuthInfo: UserAuthHeaderWriter("bearer " + userToken),
			})
		agentClient = client.New(
			client.Config{
				URL:      srvUrl,
				AuthInfo: AgentAuthHeaderWriter("fake_pull_secret"),
			})
	})

	AfterEach(func() {
		server.Close()
	})

	verifyResponseErrorCode := func(err error, expectUnauthorizedCode bool) {
		expectedCode := "403"
		if expectUnauthorizedCode {
			expectedCode = "401"
		}
		assert.Contains(t, err.Error(), expectedCode)
	}

	It("should store payload in cache", func() {
		assert.Equal(t, shouldStorePayloadInCache(nil), true)
		err := common.NewApiError(http.StatusUnauthorized, errors.New(""))
		assert.Equal(t, shouldStorePayloadInCache(err), true)
	})

	It("should not store payload in cache", func() {
		err1 := common.NewApiError(http.StatusInternalServerError, errors.New(""))
		assert.Equal(t, shouldStorePayloadInCache(err1), false)
		err2 := errors.New("internal error")
		assert.Equal(t, shouldStorePayloadInCache(err2), false)
	})

	It("pass access review from cache", func() {
		By("get cluster first attempt, store user in cache", func() {
			passAccessReview(1)
			passCapabilityReview(1)
			err := getCluster(ctx, userClient)
			Expect(err).To(BeNil())
		})
		By("get cluster second attempt, get user from cache", func() {
			passCapabilityReview(1)
			defer authzCache.Flush()
			err := getCluster(ctx, userClient)
			Expect(err).To(BeNil())
		})
	})

	It("access review failure", func() {
		failAccessReview(1)
		passCapabilityReview(1)
		defer authzCache.Flush()
		err := getCluster(ctx, userClient)
		verifyResponseErrorCode(err, false)
	})

	tests := []struct {
		name                   string
		allowedRoles           []ocm.RoleType
		apiCall                func(ctx context.Context, cli *client.AssistedInstall) error
		expectUnauthorizedCode int
		agentAuthSupport       bool
	}{
		{
			name:                   "register cluster",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                registerCluster,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "list clusters",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                listClusters,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "get cluster",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                getCluster,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "update cluster",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                updateCluster,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "deregister cluster",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                deregisterCluster,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "download cluster files",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                downloadClusterFiles,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "get presigned for cluster files",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                getPresignedForClusterFiles,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "get credentials",
			allowedRoles:           []ocm.RoleType{ocm.UserRole},
			apiCall:                getCredentials,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "download cluster kubeconfig",
			allowedRoles:           []ocm.RoleType{ocm.UserRole},
			apiCall:                downloadClusterKubeconfig,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "get cluster install config",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                getClusterInstallConfig,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "update cluster install config",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                updateClusterInstallConfig,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "upload cluster ingress cert",
			apiCall:                uploadClusterIngressCert,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "install cluster",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                installCluster,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "cancel installation",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                cancelInstallation,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "reset cluster",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                resetCluster,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "complete installation",
			apiCall:                completeInstallation,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "register host",
			apiCall:                registerHost,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "lists hosts",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                listHosts,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "get host",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                getHost,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "deregister host",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                deregisterHost,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "update host install progress",
			apiCall:                updateHostInstallProgress,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "get next steps",
			apiCall:                getNextSteps,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "post step reply",
			apiCall:                postStepReply,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "upload host logs",
			apiCall:                uploadHostLogs,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "download host logs",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                downloadHostLogs,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "upload logs",
			apiCall:                uploadLogs,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "download cluster logs",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                downloadClusterLogs,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "list events",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                listEvents,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "list managed domains",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                listManagedDomains,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "list component versions",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                listComponentVersions,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "list supported openshift versions",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                listSupportedOpenshiftVersions,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "register add hosts cluster",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                registerAddHostsCluster,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:                   "get discovery ignition",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole, ocm.UserRole},
			apiCall:                v2DownloadInfraEnvFiles,
			agentAuthSupport:       true,
			expectUnauthorizedCode: http.StatusUnauthorized,
		},
		{
			name:                   "Update discovery ignition",
			allowedRoles:           []ocm.RoleType{ocm.AdminRole, ocm.UserRole},
			apiCall:                updateDiscoveryIgnition,
			expectUnauthorizedCode: http.StatusForbidden,
		},
		{
			name:         "List support features",
			allowedRoles: []ocm.RoleType{ocm.AdminRole, ocm.UserRole, ocm.ReadOnlyAdminRole},
			apiCall:      v2ListFeatureSupportLevels,
		},
	}

	for _, tt := range tests {
		tt := tt
		It(fmt.Sprintf("test %s", tt.name), func() {
			userAuthSupport := len(tt.allowedRoles) > 0
			By(fmt.Sprintf("%s: with user scope", tt.name), func() {
				userRoleSupport := funk.Contains(tt.allowedRoles, ocm.UserRole)
				if userAuthSupport {
					failCapabilityReview(1)
					if userRoleSupport {
						passAccessReview(1)
					}
				}
				defer authzCache.Flush()
				err := tt.apiCall(ctx, userClient)
				if userRoleSupport {
					assert.Equal(t, err, nil)
				} else {
					assert.NotEqual(t, err, nil)
					assert.Contains(t, err.Error(), strconv.Itoa(tt.expectUnauthorizedCode))

				}
			})
			By(fmt.Sprintf("%s: with read-only-admin scope", tt.name), func() {
				readOnlyAdminRoleSupport := funk.Contains(tt.allowedRoles, ocm.ReadOnlyAdminRole)
				if userAuthSupport {
					passCapabilityReview(1)
					if readOnlyAdminRoleSupport {
						passAccessReview(1)
					}
				}
				defer authzCache.Flush()
				err := tt.apiCall(ctx, userClient)
				if readOnlyAdminRoleSupport {
					assert.Equal(t, err, nil)
				} else {
					assert.NotEqual(t, err, nil)
					assert.Contains(t, err.Error(), strconv.Itoa(tt.expectUnauthorizedCode))
				}
			})
			By(fmt.Sprintf("%s: with admin scope", tt.name), func() {
				adminUsers = []string{"test@user"}
				defer func() {
					adminUsers = []string{}
				}()
				adminRoleSupport := funk.Contains(tt.allowedRoles, ocm.AdminRole)
				if userAuthSupport {
					if adminRoleSupport {
						passAccessReview(1)
					}
				}
				defer authzCache.Flush()
				err := tt.apiCall(ctx, userClient)
				if adminRoleSupport {
					assert.Equal(t, err, nil)
				} else {
					assert.NotEqual(t, err, nil)
					assert.Contains(t, err.Error(), strconv.Itoa(tt.expectUnauthorizedCode))
				}
			})
			By(fmt.Sprintf("%s: with agent auth", tt.name), func() {
				if tt.agentAuthSupport {
					passAccessReview(1)
				}
				defer authzCache.Flush()
				err := tt.apiCall(ctx, agentClient)
				if tt.agentAuthSupport {
					assert.Equal(t, err, nil)
				} else {
					assert.NotEqual(t, err, nil)
					assert.Contains(t, err.Error(), strconv.Itoa(http.StatusUnauthorized))
				}
			})
		})
	}
})

func registerCluster(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2RegisterCluster(
		ctx,
		&installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}`),
			},
		})
	return err
}

func listClusters(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
	return err
}

func getCluster(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2GetCluster(
		ctx,
		&installer.V2GetClusterParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func updateCluster(ctx context.Context, cli *client.AssistedInstall) error {
	dnsDomain := "a.com"
	_, err := cli.Installer.V2UpdateCluster(
		ctx,
		&installer.V2UpdateClusterParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				BaseDNSDomain: &dnsDomain,
			}})
	return err
}

func deregisterCluster(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2DeregisterCluster(
		ctx,
		&installer.V2DeregisterClusterParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func downloadClusterFiles(ctx context.Context, cli *client.AssistedInstall) error {
	file, err := os.CreateTemp("", "test")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())

	_, err = cli.Installer.V2DownloadClusterFiles(
		ctx,
		&installer.V2DownloadClusterFilesParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			FileName:  "bootstrap.ign",
		},
		file)
	return err
}

func getPresignedForClusterFiles(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2GetPresignedForClusterFiles(
		ctx,
		&installer.V2GetPresignedForClusterFilesParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			FileName:  "bootstrap.ign",
		})
	return err
}

func getCredentials(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2GetCredentials(
		ctx,
		&installer.V2GetCredentialsParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func downloadClusterKubeconfig(ctx context.Context, cli *client.AssistedInstall) error {
	file, err := os.CreateTemp("", "test")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())

	_, err = cli.Installer.V2DownloadClusterCredentials(
		ctx,
		&installer.V2DownloadClusterCredentialsParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			FileName:  "kubeconfig",
		},
		file)
	return err
}

func getClusterInstallConfig(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2GetClusterInstallConfig(
		ctx,
		&installer.V2GetClusterInstallConfigParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func updateClusterInstallConfig(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2UpdateClusterInstallConfig(
		ctx,
		&installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           strfmt.UUID(uuid.New().String()),
			InstallConfigParams: `{"controlPlane": {"hyperthreading": "Disabled"}}`,
		})
	return err
}

func uploadClusterIngressCert(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2UploadClusterIngressCert(
		ctx,
		&installer.V2UploadClusterIngressCertParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func installCluster(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2InstallCluster(
		ctx,
		&installer.V2InstallClusterParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func cancelInstallation(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2CancelInstallation(
		ctx,
		&installer.V2CancelInstallationParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func resetCluster(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2ResetCluster(
		ctx,
		&installer.V2ResetClusterParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func completeInstallation(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2CompleteInstallation(
		ctx,
		&installer.V2CompleteInstallationParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			CompletionParams: &models.CompletionParams{
				IsSuccess: swag.Bool(true),
				ErrorInfo: "",
			},
		})
	return err
}

func registerHost(ctx context.Context, cli *client.AssistedInstall) error {
	hostId := strfmt.UUID(uuid.New().String())
	_, err := cli.Installer.V2RegisterHost(
		ctx,
		&installer.V2RegisterHostParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			NewHostParams: &models.HostCreateParams{
				HostID: &hostId,
			},
		})
	return err
}

func listHosts(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2ListHosts(
		ctx,
		&installer.V2ListHostsParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
		})
	return err
}

func getHost(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2GetHost(
		ctx,
		&installer.V2GetHostParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			HostID:     strfmt.UUID(uuid.New().String()),
		})
	return err
}

func deregisterHost(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2DeregisterHost(
		ctx,
		&installer.V2DeregisterHostParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			HostID:     strfmt.UUID(uuid.New().String()),
		})
	return err
}

func updateHostInstallProgress(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2UpdateHostInstallProgress(
		ctx,
		&installer.V2UpdateHostInstallProgressParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			HostID:     strfmt.UUID(uuid.New().String()),
			HostProgress: &models.HostProgress{
				CurrentStage: models.HostStageStartingInstallation,
			},
		})
	return err
}

func getNextSteps(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2GetNextSteps(
		ctx,
		&installer.V2GetNextStepsParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			HostID:     strfmt.UUID(uuid.New().String()),
		})
	return err
}

func postStepReply(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2PostStepReply(
		ctx,
		&installer.V2PostStepReplyParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			HostID:     strfmt.UUID(uuid.New().String()),
		})
	return err
}

func uploadHostLogs(ctx context.Context, cli *client.AssistedInstall) error {
	hostId := strfmt.UUID(uuid.New().String())
	file, err := os.CreateTemp("", "test")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())

	_, err = cli.Installer.V2UploadLogs(
		ctx,
		&installer.V2UploadLogsParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			HostID:    &hostId,
			Upfile:    file,
			LogsType:  string(models.LogsTypeHost),
		})
	return err
}

func downloadHostLogs(ctx context.Context, cli *client.AssistedInstall) error {
	logsType := string(models.LogsTypeHost)
	hostId := strfmt.UUID(uuid.New().String())
	file, err := os.CreateTemp("", "test")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())

	_, err = cli.Installer.V2DownloadClusterLogs(
		ctx,
		&installer.V2DownloadClusterLogsParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			HostID:    &hostId,
			LogsType:  &logsType,
		},
		file)
	return err
}

func uploadLogs(ctx context.Context, cli *client.AssistedInstall) error {
	file, err := os.CreateTemp("", "test")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())

	hostId := strfmt.UUID(uuid.New().String())
	_, err = cli.Installer.V2UploadLogs(
		ctx,
		&installer.V2UploadLogsParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			HostID:    &hostId,
			LogsType:  string(models.LogsTypeController),
			Upfile:    file,
		})
	return err
}

func downloadClusterLogs(ctx context.Context, cli *client.AssistedInstall) error {
	logsType := string(models.LogsTypeController)
	file, err := os.CreateTemp("", "test")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())

	_, err = cli.Installer.V2DownloadClusterLogs(
		ctx,
		&installer.V2DownloadClusterLogsParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			LogsType:  &logsType,
		},
		file)
	return err
}

func listEvents(ctx context.Context, cli *client.AssistedInstall) error {
	clusterId := strfmt.UUID(uuid.New().String())
	hostId := strfmt.UUID(uuid.New().String())
	_, err := cli.Events.V2ListEvents(
		ctx,
		&events.V2ListEventsParams{
			ClusterID: &clusterId,
			HostID:    &hostId,
		})
	return err
}

func listManagedDomains(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.ManagedDomains.V2ListManagedDomains(
		ctx,
		&managed_domains.V2ListManagedDomainsParams{})
	return err
}

func listComponentVersions(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Versions.V2ListComponentVersions(
		ctx,
		&versions.V2ListComponentVersionsParams{})
	return err
}

func listSupportedOpenshiftVersions(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Versions.V2ListSupportedOpenshiftVersions(
		ctx,
		&versions.V2ListSupportedOpenshiftVersionsParams{})
	return err
}

func registerAddHostsCluster(ctx context.Context, cli *client.AssistedInstall) error {
	clusterName := "add-hosts-cluster"
	apiVIPDnsname := "api-vip.redhat.com"
	openshiftClusterID := strfmt.UUID(uuid.New().String())
	_, err := cli.Installer.V2ImportCluster(
		ctx,
		&installer.V2ImportClusterParams{
			NewImportClusterParams: &models.ImportClusterParams{
				APIVipDnsname:      &apiVIPDnsname,
				Name:               &clusterName,
				OpenshiftVersion:   common.TestDefaultConfig.OpenShiftVersion,
				OpenshiftClusterID: &openshiftClusterID,
			},
		})
	return err
}

func v2DownloadInfraEnvFiles(ctx context.Context, cli *client.AssistedInstall) error {
	file, err := os.CreateTemp("", "test")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())

	_, err = cli.Installer.V2DownloadInfraEnvFiles(
		ctx,
		&installer.V2DownloadInfraEnvFilesParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			FileName:   "discovery.ign",
		},
		file)
	return err
}

func updateDiscoveryIgnition(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.UpdateInfraEnv(
		ctx,
		&installer.UpdateInfraEnvParams{
			InfraEnvID:           strfmt.UUID(uuid.New().String()),
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: ""},
		})
	return err
}

func v2ListFeatureSupportLevels(ctx context.Context, cli *client.AssistedInstall) error {
	_, err := cli.Installer.V2ListFeatureSupportLevels(ctx, &installer.V2ListFeatureSupportLevelsParams{})
	return err
}

var _ = Describe("imageTokenAuthorizer", func() {
	var (
		a   *AuthzHandler
		ctx context.Context
	)

	BeforeEach(func() {
		log := logrus.New()
		log.SetOutput(io.Discard)
		a = &AuthzHandler{log: log.WithField("pkg", "auth")}
		ctx = context.Background()
	})

	It("succeeds when the request id matches the claim id", func() {
		id := "ed172693-7c24-4add-8dfc-2bfa536b0cbb"
		claims := jwt.MapClaims{"sub": id}
		ctx = context.WithValue(ctx, restapi.AuthKey, claims)
		ctx = params.SetParam(ctx, "infra_env_id", id)

		Expect(a.imageTokenAuthorizer(ctx)).To(Succeed())
	})

	It("fails if the auth payload is missing", func() {
		id := "ed172693-7c24-4add-8dfc-2bfa536b0cbb"
		ctx = params.SetParam(ctx, "infra_env_id", id)

		Expect(a.imageTokenAuthorizer(ctx)).NotTo(Succeed())
	})

	It("fails if the claims are the wrong type", func() {
		id := "ed172693-7c24-4add-8dfc-2bfa536b0cbb"
		claims := map[string]string{"sub": id}
		ctx = context.WithValue(ctx, restapi.AuthKey, claims)
		ctx = params.SetParam(ctx, "infra_env_id", id)

		Expect(a.imageTokenAuthorizer(ctx)).NotTo(Succeed())
	})

	It("fails if the sub claim is missing", func() {
		ctx = context.WithValue(ctx, restapi.AuthKey, jwt.MapClaims{})
		ctx = params.SetParam(ctx, "infra_env_id", "ed172693-7c24-4add-8dfc-2bfa536b0cbb")
		Expect(a.imageTokenAuthorizer(ctx)).NotTo(Succeed())
	})

	It("fails if the infraEnv ID isn't in the request", func() {
		id := "ed172693-7c24-4add-8dfc-2bfa536b0cbb"
		claims := jwt.MapClaims{"sub": id}
		ctx = context.WithValue(ctx, restapi.AuthKey, claims)

		Expect(a.imageTokenAuthorizer(ctx)).NotTo(Succeed())
	})

	It("fails if the request id doesn't match the claim id", func() {
		claims := jwt.MapClaims{"sub": "ed172693-7c24-4add-8dfc-2bfa536b0cbb"}
		ctx = context.WithValue(ctx, restapi.AuthKey, claims)
		ctx = params.SetParam(ctx, "infra_env_id", "76c37ebc-94e5-4ddf-bcf8-3cf27c5edab6")

		Expect(a.imageTokenAuthorizer(ctx)).NotTo(Succeed())
	})
})
