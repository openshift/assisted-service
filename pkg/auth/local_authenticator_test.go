package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ = Describe("AuthAgentAuth", func() {
	var (
		a        *LocalAuthenticator
		infraEnv *common.InfraEnv
		db       *gorm.DB
		dbName   string
		token    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		infraEnvID := strfmt.UUID(uuid.New().String())
		infraEnv = &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}
		Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())

		pubKey, privKey, err := gencrypto.ECDSAKeyPairPEM()
		Expect(err).ToNot(HaveOccurred())

		cfg := &Config{ECPublicKeyPEM: pubKey}

		token, err = gencrypto.LocalJWTForKey(infraEnvID.String(), privKey, gencrypto.InfraEnvKey)
		Expect(err).ToNot(HaveOccurred())

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

	validateErrorResponse := func(err error) {
		infraError, ok := err.(*common.InfraErrorResponse)
		Expect(ok).To(BeTrue())
		Expect(infraError.StatusCode()).To(Equal(int32(401)))
	}

	It("Validates a token correctly", func() {
		_, err := a.AuthAgentAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Caches the response", func() {
		_, err := a.AuthAgentAuth(token)
		Expect(err).ToNot(HaveOccurred())

		resp := db.Delete(infraEnv)
		Expect(resp.Error).ToNot(HaveOccurred())

		_, err = a.AuthAgentAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails an invalid token", func() {
		_, err := a.AuthAgentAuth(token + "asdf")
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

	It("Fails all user auth", func() {
		_, err := a.AuthUserAuth(token)
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

	It("Fails with watcher auth", func() {
		_, err := a.AuthWatcherAuth(token)
		Expect(err).To(HaveOccurred())
	})

	It("Fails a token with invalid signing method", func() {
		newTok := fakeTokenAlg(token)
		_, err := a.AuthAgentAuth(newTok)
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

	It("Fails with an RSA token", func() {
		rsaToken, _ := GetTokenAndCert(false)
		_, err := a.AuthAgentAuth(rsaToken)
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

	It("Fails for a deleted infraEnv", func() {
		resp := db.Delete(infraEnv)
		Expect(resp.Error).ToNot(HaveOccurred())

		_, err := a.AuthAgentAuth(token)
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})
})
