package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	commontesting "github.com/openshift/assisted-service/internal/common/testing"
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	emailDomain      = "example.com"
	pullSecretFormat = `{"auths":{"cloud.openshift.com":{"auth":"%s","email":"user@example.com"}}}` // #nosec
	username         = "theUsername"
)

var _ = Describe("setHeaders", func() {
	const (
		serviceVersion = "v1.0.0-efah21a"
	)

	var (
		ctrl          *gomock.Controller
		db            *gorm.DB
		dbName        string
		token         string
		clusterID     strfmt.UUID
		mockK8sClient *k8sclient.MockK8SClient
		uploader      *eventsUploader
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		mockK8sClient = k8sclient.NewMockK8SClient(ctrl)

		cfg := &Config{
			AssistedServiceVersion: serviceVersion,
		}
		uploader = &eventsUploader{
			db:     db,
			client: mockK8sClient,
			Config: *cfg,
		}
		token = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, "thePassword")))

	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("correctly sets the request header with a valid auth token", func() {
		req, err := http.NewRequest(http.MethodPost, "http://example.com", bytes.NewBuffer([]byte("data")))
		Expect(err).NotTo(HaveOccurred())
		Expect(req).NotTo(BeNil())
		err = uploader.setHeaders(req, &clusterID, token, "multipart-form")
		Expect(err).NotTo(HaveOccurred())
		checkHeaders(req, token, serviceVersion, clusterID)
	})
	It("fails to set the request header with an empty auth token", func() {
		req, err := http.NewRequest(http.MethodPost, "http://example.com", bytes.NewBuffer([]byte("data")))
		Expect(err).NotTo(HaveOccurred())
		Expect(req).NotTo(BeNil())
		err = uploader.setHeaders(req, &clusterID, "", "multipart-form")
		Expect(err).To(HaveOccurred())
		Expect(req.Header).To(BeEmpty())
	})
})

var _ = Describe("prepareBody", func() {
	It("successfully creates the request body", func() {
		By("preparing the data for the request body")
		clusterID := strfmt.UUID(uuid.NewString())
		cluster := models.Cluster{
			ID: &clusterID,
		}
		clusterJson, err := json.Marshal(cluster)
		Expect(err).NotTo(HaveOccurred())

		buffer := &bytes.Buffer{}
		tw := tar.NewWriter(buffer)
		Expect(addFile(tw, clusterJson,
			fmt.Sprintf("%s/events/cluster.json", clusterID))).ShouldNot(HaveOccurred())
		Expect(tw.Close()).ShouldNot(HaveOccurred())

		By("calling the prepareBody function")
		buf, formType, err := prepareBody(buffer)
		Expect(err).NotTo(HaveOccurred())
		Expect(buf.Bytes()).NotTo(BeEmpty())
		Expect(formType).NotTo(BeEmpty())

		By("checking the output of prepareBody")
		parts := strings.Split(formType, "boundary=")
		rdr := multipart.NewReader(buf, parts[1])
		testFiles := map[string]*testFile{"cluster": {expected: true}}
		for {
			nextPart, err := rdr.NextPart()
			if err == io.EOF {
				break
			}
			Expect(err).NotTo(HaveOccurred())
			tr := tar.NewReader(nextPart)
			readFiles(tr, testFiles, clusterID)
			checkFileContents(testFiles["cluster"], clusterJson)
		}
	})
	It("fails to create the request body when there's no data", func() {
		buf, formType, err := prepareBody(&bytes.Buffer{})
		Expect(err).To(HaveOccurred())
		Expect(buf).To(BeNil())
		Expect(formType).To(BeEmpty())
	})
	It("fails to create the request body when given an empty buffer", func() {
		buf, formType, err := prepareBody(nil)
		Expect(err).To(HaveOccurred())
		Expect(buf).To(BeNil())
		Expect(formType).To(BeEmpty())
	})
})

var _ = Describe("prepareFiles", func() {
	var (
		ctrl       *gomock.Controller
		ctx        context.Context
		db         *gorm.DB
		dbName     string
		token      string
		clusterID  strfmt.UUID
		mockEvents *eventsapi.MockHandler
		hostID     strfmt.UUID
		infraEnvID strfmt.UUID
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.Background()
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		infraEnvID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		token = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, "thePassword")))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("successfully prepares all files", func() {
		mockEvents.EXPECT().V2GetEvents(
			ctx, &clusterID, nil, nil, models.EventCategoryMetrics, models.EventCategoryUser).Return([]*common.Event{}, nil).Times(1)

		cluster := createTestObjects(db, &clusterID, &hostID, &infraEnvID)
		pullSecret := validations.PullSecretCreds{AuthRaw: token, Email: fmt.Sprintf("testemail@%s", emailDomain), Username: username}
		buf, err := prepareFiles(ctx, db, cluster, mockEvents, &pullSecret)
		Expect(err).NotTo(HaveOccurred())
		Expect(buf.Bytes()).NotTo(BeEmpty())
		testFiles := map[string]*testFile{
			"cluster":  {expected: true},
			"infraenv": {expected: true},
			"hosts":    {expected: true},
			"events":   {expected: true},
		}

		readtgzFiles(testFiles, clusterID, buf.Bytes())
		checkHostsFile(db, testFiles["hosts"], clusterID)
		checkClusterFile(db, testFiles["cluster"], clusterID, username, emailDomain)
		checkInfraEnvFile(db, testFiles["infraenv"], infraEnvID)
		checkEventsFile(testFiles["events"], []string{}, 0)
	})
	It("prepares only the event data for the current cluster", func() {
		clusterID2 := strfmt.UUID(uuid.New().String())
		eventsHandler := events.New(db, nil, commontesting.GetDummyNotificationStream(ctrl), common.GetTestLog())
		eventsHandler.V2AddEvent(ctx, &clusterID, &hostID, &infraEnvID, models.ClusterStatusAddingHosts, models.EventSeverityInfo, "adding hosts", time.Now())
		eventsHandler.V2AddEvent(ctx, &clusterID2, &hostID, &infraEnvID, models.ClusterStatusError, models.EventSeverityInfo, "fake event", time.Now())
		cluster := createTestObjects(db, &clusterID, &hostID, &infraEnvID)
		createTestObjects(db, &clusterID2, nil, nil)
		pullSecret := validations.PullSecretCreds{AuthRaw: token, Email: fmt.Sprintf("testemail@%s", emailDomain), Username: username}
		buf, err := prepareFiles(ctx, db, cluster, eventsHandler, &pullSecret)
		Expect(err).NotTo(HaveOccurred())
		Expect(buf.Bytes()).NotTo(BeEmpty())
		testFiles := map[string]*testFile{
			"cluster":  {expected: true},
			"infraenv": {expected: true},
			"hosts":    {expected: true},
			"events":   {expected: true},
		}

		readtgzFiles(testFiles, clusterID, buf.Bytes())
		checkHostsFile(db, testFiles["hosts"], clusterID)
		checkClusterFile(db, testFiles["cluster"], clusterID, username, emailDomain)
		checkInfraEnvFile(db, testFiles["infraenv"], infraEnvID)
		checkEventsFile(testFiles["events"], []string{models.ClusterStatusAddingHosts}, 1)
	})
	It("prepares only the cluster, host, and event data when missing infraEnv ID", func() {
		mockEvents.EXPECT().V2GetEvents(
			ctx, &clusterID, nil, nil, models.EventCategoryMetrics, models.EventCategoryUser).Return([]*common.Event{}, nil).Times(1)

		cluster := createTestObjects(db, &clusterID, &hostID, nil)
		pullSecret := validations.PullSecretCreds{AuthRaw: token, Email: fmt.Sprintf("testemail@%s", emailDomain), Username: username}
		buf, err := prepareFiles(ctx, db, cluster, mockEvents, &pullSecret)
		Expect(err).NotTo(HaveOccurred())
		Expect(buf.Bytes()).NotTo(BeEmpty())
		testFiles := map[string]*testFile{
			"cluster":  {expected: true},
			"infraenv": {expected: false},
			"hosts":    {expected: true},
			"events":   {expected: true},
		}

		readtgzFiles(testFiles, clusterID, buf.Bytes())
		checkHostsFile(db, testFiles["hosts"], clusterID)
		checkClusterFile(db, testFiles["cluster"], clusterID, username, emailDomain)
		checkInfraEnvFile(db, testFiles["infraenv"], infraEnvID)
		checkEventsFile(testFiles["events"], []string{}, 0)
	})
	It("fails to prepare files when there is no data", func() {
		mockEvents.EXPECT().V2GetEvents(ctx, nil, nil, nil, models.EventCategoryMetrics, models.EventCategoryUser).Return(
			nil, errors.New("no events found")).Times(1)
		pullSecret := validations.PullSecretCreds{AuthRaw: token, Email: fmt.Sprintf("testemail@%s", emailDomain)}
		buf, err := prepareFiles(ctx, db, &common.Cluster{}, mockEvents, &pullSecret)
		Expect(err).To(HaveOccurred())
		Expect(buf).To(BeNil())
		testFiles := map[string]*testFile{
			"cluster":  {expected: false},
			"infraenv": {expected: false},
			"hosts":    {expected: false},
			"events":   {expected: false},
		}

		readtgzFiles(testFiles, clusterID, nil)
		checkHostsFile(db, testFiles["hosts"], clusterID)
		checkClusterFile(db, testFiles["cluster"], clusterID, username, emailDomain)
		checkInfraEnvFile(db, testFiles["infraenv"], infraEnvID)
		checkEventsFile(testFiles["events"], []string{}, 0)
	})
})

var _ = Describe("UploadEvents", func() {
	const (
		serviceVersion = "v1.0.0-efah21a"
	)

	var (
		ctx              context.Context
		ctrl             *gomock.Controller
		db               *gorm.DB
		dbName           string
		token            string
		clusterID        strfmt.UUID
		hostID           strfmt.UUID
		infraEnvID       strfmt.UUID
		mockK8sClient    *k8sclient.MockK8SClient
		uploader         *eventsUploader
		dataUploadServer func([]string, int, map[string]*testFile) http.HandlerFunc
		mockEvents       *eventsapi.MockHandler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		mockK8sClient = k8sclient.NewMockK8SClient(ctrl)
		ctx = context.Background()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		infraEnvID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		token = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, "thePassword")))

		dataUploadServer = func(expectedEvents []string, expectedNumberOfEvents int, testFiles map[string]*testFile) http.HandlerFunc {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.URL.Path).To(Equal("/upload/test"))
				Expect(r.Body).NotTo(BeNil())
				checkHeaders(r, token, serviceVersion, clusterID)
				mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.HasPrefix(mediaType, "multipart")).To(BeTrue())
				rdr := multipart.NewReader(r.Body, params["boundary"])
				for {
					nextPart, err := rdr.NextPart()
					if err == io.EOF {
						break
					}
					Expect(err).NotTo(HaveOccurred())
					contents, err := io.ReadAll(nextPart)
					Expect(err).NotTo(HaveOccurred())
					readtgzFiles(testFiles, clusterID, contents)
					checkClusterFile(db, testFiles["cluster"], clusterID, username, emailDomain)
					checkInfraEnvFile(db, testFiles["infraenv"], infraEnvID)
					checkHostsFile(db, testFiles["hosts"], clusterID)
					checkEventsFile(testFiles["events"], expectedEvents, expectedNumberOfEvents)
				}
			})
		}
		cfg := &Config{
			AssistedServiceVersion: serviceVersion,
		}
		uploader = &eventsUploader{
			db:     db,
			client: mockK8sClient,
			Config: *cfg,
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
	It("successfully uploads event data", func() {
		event := models.Event{
			Category:  models.EventCategoryUser,
			ClusterID: &clusterID,
			Name:      models.ClusterStatusAddingHosts,
		}
		createOCMPullSecret(*mockK8sClient, true)
		mockEvents.EXPECT().V2GetEvents(
			ctx, &clusterID, nil, nil, models.EventCategoryMetrics, models.EventCategoryUser).Return(
			[]*common.Event{{Event: event}}, nil).Times(1)
		testFiles := map[string]*testFile{
			"cluster":  {expected: true},
			"hosts":    {expected: true},
			"infraenv": {expected: true},
			"events":   {expected: true},
		}
		server := httptest.NewServer(dataUploadServer([]string{models.ClusterStatusAddingHosts}, 1, testFiles))
		uploader.Config.DataUploadEndpoint = fmt.Sprintf("%s/%s", server.URL, "upload/test")

		cluster := createTestObjects(db, &clusterID, &hostID, &infraEnvID)
		err := uploader.UploadEvents(ctx, cluster, mockEvents)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("fails to upload event data when there's no data", func() {
		createOCMPullSecret(*mockK8sClient, true)
		mockEvents.EXPECT().V2GetEvents(ctx, nil, nil, nil, models.EventCategoryMetrics, models.EventCategoryUser).Return(
			nil, errors.New("no events found")).Times(1)
		err := uploader.UploadEvents(ctx, &common.Cluster{PullSecret: ""}, mockEvents)
		Expect(err).To(HaveOccurred())
	})
	It("fails to uploads event data when headers can't be set", func() {
		createOCMPullSecret(*mockK8sClient, false)
		mockEvents.EXPECT().V2GetEvents(
			ctx, &clusterID, nil, nil, models.EventCategoryMetrics, models.EventCategoryUser).Return([]*common.Event{}, nil).Times(1)

		cluster := createTestObjects(db, &clusterID, &hostID, &infraEnvID)
		err := uploader.UploadEvents(ctx, cluster, mockEvents)
		Expect(err).To(HaveOccurred())
	})
})

func createTestObjects(db *gorm.DB, clusterID, hostID, infraEnvID *strfmt.UUID) *common.Cluster {
	infraEnv := common.InfraEnv{
		InfraEnv: models.InfraEnv{
			ID: infraEnvID,
		},
	}
	if infraEnvID != nil {
		Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
	}
	host := common.Host{
		Host: models.Host{
			ID: hostID,
		},
	}
	if hostID != nil {
		if infraEnvID != nil {
			host.Host.InfraEnvID = *infraEnvID
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	}
	cluster := common.Cluster{
		Cluster: models.Cluster{
			ID:    clusterID,
			Hosts: []*models.Host{},
		},
	}
	if clusterID != nil {
		if hostID != nil {
			cluster.Cluster.Hosts = append(cluster.Cluster.Hosts, &host.Host)
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		dbCluster, err := common.GetClusterFromDBWithHosts(db, *clusterID)
		Expect(err).ShouldNot(HaveOccurred())
		return dbCluster

	}
	return nil
}

func checkFileContents(testFile *testFile, expectedContents []byte) {
	Expect(testFile.expected).To(Equal(testFile.exists))
	Expect(string(testFile.contents)).To(Equal(string(expectedContents)))
}

type testFile struct {
	expected bool
	exists   bool
	contents []byte
}

func readtgzFiles(testFiles map[string]*testFile, clusterID strfmt.UUID, buffer []byte) {
	if buffer != nil {
		gzipReader, err := gzip.NewReader(bytes.NewReader(buffer))
		Expect(err).NotTo(HaveOccurred())
		tarReader := tar.NewReader(gzipReader)
		readFiles(tarReader, testFiles, clusterID)
	}
}

func readFiles(tr *tar.Reader, testFiles map[string]*testFile, clusterID strfmt.UUID) {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		var fileName string
		switch hdr.Name {
		case fmt.Sprintf("%s/events/hosts.json", clusterID):
			fileName = "hosts"
		case fmt.Sprintf("%s/events/infraenv.json", clusterID):
			fileName = "infraenv"
		case fmt.Sprintf("%s/events/cluster.json", clusterID):
			fileName = "cluster"
		case fmt.Sprintf("%s/events.json", clusterID):
			fileName = "events"
		}
		if fileName != "" {
			fileContents, err := io.ReadAll(tr)
			Expect(err).NotTo(HaveOccurred())
			testFiles[fileName].contents = fileContents
			testFiles[fileName].exists = true
		}
	}
}
func createOCMPullSecret(mockK8sClient k8sclient.MockK8SClient, exists bool) {
	if exists {
		token := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, "thePassword")))
		OCMSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "openshift-config",
				Name:      "pull-secret",
			},
			Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(fmt.Sprintf(pullSecretFormat, token))},
			Type: corev1.SecretTypeDockerConfigJson,
		}
		mockK8sClient.EXPECT().GetSecret("openshift-config", "pull-secret").Return(OCMSecret, nil).Times(1)
	} else {
		mockK8sClient.EXPECT().GetSecret("openshift-config", "pull-secret").Return(nil,
			apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "Secret"}, "pullsecret")).Times(1)
	}
}

func checkHeaders(req *http.Request, token, serviceVersion string, clusterID strfmt.UUID) {
	Expect(req.Header).NotTo(BeEmpty())
	userAgentHeader := req.UserAgent()
	Expect(userAgentHeader).NotTo(BeEmpty())
	Expect(userAgentHeader).To(Equal(fmt.Sprintf("assisted-installer-operator/%s cluster/%s", serviceVersion, clusterID)))
	Expect(req.Header).To(HaveKey("Content-Type"))
	Expect(req.Header).To(HaveKeyWithValue("Authorization", []string{fmt.Sprintf("Bearer %s", token)}))
}

func checkHostsFile(db *gorm.DB, hostFile *testFile, clusterID strfmt.UUID) {
	var expectedContents []byte
	if hostFile.expected {
		cluster, err := common.GetClusterFromDBWithHosts(db, clusterID)
		Expect(err).NotTo(HaveOccurred())
		expectedContents, err = json.MarshalIndent(cluster.Cluster.Hosts, "", " ")
		Expect(err).NotTo(HaveOccurred())
	}
	checkFileContents(hostFile, expectedContents)
}
func checkInfraEnvFile(db *gorm.DB, infraenvFile *testFile, infraEnvID strfmt.UUID) {
	var expectedContents []byte
	if infraenvFile.expected {
		infraEnv, err := common.GetInfraEnvFromDB(db, infraEnvID)
		Expect(err).NotTo(HaveOccurred())
		expectedContents, err = json.Marshal(infraEnv.InfraEnv)
		Expect(err).NotTo(HaveOccurred())
	}
	checkFileContents(infraenvFile, expectedContents)
}

func checkClusterFile(db *gorm.DB, clusterFile *testFile, clusterID strfmt.UUID, username, emailDomain string) {
	var expectedContents []byte
	if clusterFile.expected {
		cluster, err := common.GetClusterFromDBWithHosts(db, clusterID)
		if username != "" {
			cluster.UserName = username
		}
		if emailDomain != "" {
			cluster.EmailDomain = emailDomain
		}
		Expect(err).NotTo(HaveOccurred())
		expectedContents, err = json.Marshal(cluster.Cluster)
		Expect(err).NotTo(HaveOccurred())
	}
	checkFileContents(clusterFile, expectedContents)
}

func checkEventsFile(eventsFile *testFile, expectedEvents []string, expectedNumberOfEvents int) {
	Expect(eventsFile.expected).To(Equal(eventsFile.exists))
	if eventsFile.expected {
		var events []*models.Event
		Expect(json.Unmarshal(eventsFile.contents, &events)).ShouldNot(HaveOccurred())
		Expect(events).To(HaveLen(expectedNumberOfEvents))
		var dbEvents []*common.Event
		for _, event := range events {
			dbEvents = append(dbEvents, &common.Event{Event: *event})
		}
		for _, expectedEvent := range expectedEvents {
			foundEvent := eventstest.FindEventByName(dbEvents, expectedEvent)
			Expect(foundEvent).NotTo(BeNil())
		}
	}
}
