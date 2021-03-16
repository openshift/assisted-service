package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("AuthAgentAuth", func() {
	var (
		a       *LocalAuthenticator
		cluster *common.Cluster
		db      *gorm.DB
		dbName  = "local_auth_test"
		key     string
		token   string
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{ID: &clusterID}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		var err error
		token, key, err = ECDSATokenAndKey(clusterID.String())
		Expect(err).ToNot(HaveOccurred())

		cfg := &Config{ECPublicKeyPEM: key}

		a, err = NewLocalAuthenticator(cfg, logrus.New(), db)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	fakeTokenAlg := func(t string) string {
		parts := strings.Split(t, ".")

		headerJSON, err := base64.RawStdEncoding.DecodeString(parts[0])
		Expect(err).ToNot(HaveOccurred())

		header := &map[string]interface{}{}
		err = json.Unmarshal(headerJSON, header)
		Expect(err).ToNot(HaveOccurred())

		// change the algorithm in an otherwise valid token
		(*header)["alg"] = "RS256"

		headerBytes, err := json.Marshal(header)
		Expect(err).ToNot(HaveOccurred())
		newHeaderString := base64.RawStdEncoding.EncodeToString(headerBytes)

		parts[0] = newHeaderString
		return strings.Join(parts, ".")
	}

	It("Validates a token correctly", func() {
		_, err := a.AuthAgentAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails an invalid token", func() {
		_, err := a.AuthAgentAuth(token + "asdf")
		Expect(err).To(HaveOccurred())
	})

	It("Fails all user auth", func() {
		_, err := a.AuthUserAuth(token)
		Expect(err).To(HaveOccurred())
	})

	It("Fails a token with invalid signing method", func() {
		newTok := fakeTokenAlg(token)
		_, err := a.AuthAgentAuth(newTok)
		Expect(err).To(HaveOccurred())
	})

	It("Fails with an RSA token", func() {
		rsaToken, _ := GetTokenAndCert()
		_, err := a.AuthAgentAuth(rsaToken)
		Expect(err).To(HaveOccurred())
	})

	It("Fails for a deleted cluster", func() {
		resp := db.Delete(cluster)
		Expect(resp.Error).ToNot(HaveOccurred())

		_, err := a.AuthAgentAuth(token)
		Expect(err).To(HaveOccurred())
	})
})
