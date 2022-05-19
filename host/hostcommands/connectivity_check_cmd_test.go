package hostcommands

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("connectivitycheckcmd", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var connectivityCheckCmd *connectivityCheckCmd
	var id, clusterId, infraEnvId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		connectivityCheckCmd = NewConnectivityCheckCmd(common.GetTestLog(), db, nil, "quay.io/example/connectivity_check:latest")

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		stepReply, stepErr = connectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step_unknow_cluster_id", func() {
		clusterID := strfmt.UUID(uuid.New().String())
		host.ClusterID = &clusterID
		stepReply, stepErr = connectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})

var _ = Describe("isAdmitted", func() {
	type requester struct {
		index          int
		expectedResult bool
	}
	tests := []struct {
		name                 string
		numWaiting           int
		numAdmitted          int
		requesters           []requester
		admittedDurationSecs int
	}{
		{
			name: "Single",
			requesters: []requester{
				{
					index:          500,
					expectedResult: true,
				},
			},
		},
		{
			name:       "Waiting full",
			numWaiting: 200,
			requesters: []requester{
				{
					index:          100,
					expectedResult: true,
				},
				{
					index:          200,
					expectedResult: false,
				},
				{
					index:          150,
					expectedResult: false,
				},
				{
					index:          149,
					expectedResult: true,
				},
			},
		},
		{
			name:        "Admitted full",
			numAdmitted: 150,
			requesters: []requester{
				{
					index:          0,
					expectedResult: false,
				},
			},
		},
		{
			name:                 "Admitted full - old",
			numAdmitted:          150,
			admittedDurationSecs: 61,
			requesters: []requester{
				{
					index:          0,
					expectedResult: true,
				},
				{
					index:          149,
					expectedResult: true,
				},
				{
					index:          500,
					expectedResult: true,
				},
			},
		},
		{
			name:                 "Filling",
			numWaiting:           140,
			numAdmitted:          9,
			admittedDurationSecs: 30,
			requesters: []requester{
				{
					index:          500,
					expectedResult: true,
				},
				{
					index:          149,
					expectedResult: false,
				},
				{
					index:          139,
					expectedResult: true,
				},
				{
					index:          0,
					expectedResult: true,
				},
			},
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			clusterId := strfmt.UUID(uuid.New().String())
			infraenvId := strfmt.UUID(uuid.New().String())
			hostForIndex := func(index int) *models.Host {
				id := strfmt.UUID(fmt.Sprintf("00000000-0000-0000-0000-0000000%05x", index))
				return &models.Host{
					ID:         &id,
					InfraEnvID: infraenvId,
					ClusterID:  &clusterId,
				}
			}
			cmd := NewConnectivityCheckCmd(common.GetTestLog(), nil, nil, "quay.io/example/connectivity_check:latest")
			queue := cmd.queue
			value := &clusterQueue{}
			if t.numWaiting > 0 || t.numAdmitted > 0 {
				queue.Set(clusterId.String(), value)
			}
			for j := 0; j != t.numWaiting; j++ {
				value.waitingQueue = append(value.waitingQueue, common.GetHostKey(hostForIndex(j)))
			}
			timestamp := time.Now().Add(-(time.Duration(t.admittedDurationSecs) * time.Second))
			for j := 0; j != t.numAdmitted; j++ {
				value.admittedQueue = append(value.admittedQueue, timestamp)
			}
			for j := range t.requesters {
				r := t.requesters[j]
				Expect(cmd.isAdmitted(hostForIndex(r.index))).To(Equal(r.expectedResult))
			}
		})
	}
})
