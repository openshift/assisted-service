package cluster

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
)

var _ = Describe("AgentToken", func() {
	var (
		id strfmt.UUID
	)

	BeforeEach(func() {
		id = strfmt.UUID(uuid.New().String())
	})

	It("fails with rhsso auth when the cloud.openshift.com pull secret is missing", func() {
		c := &common.Cluster{
			Cluster:    models.Cluster{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(c, auth.TypeRHSSO)

		Expect(err).To(HaveOccurred())
	})

	It("succeeds with rhsso auth when cloud.openshift.com pull secret is present", func() {
		c := &common.Cluster{
			Cluster:    models.Cluster{ID: &id},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(c, auth.TypeRHSSO)

		Expect(err).ToNot(HaveOccurred())
	})

	It("returns empty when no auth is configured", func() {
		c := &common.Cluster{
			Cluster:    models.Cluster{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		token, err := AgentToken(c, auth.TypeNone)
		Expect(err).ToNot(HaveOccurred())
		Expect(token).To(Equal(""))
	})

	It("returns an error if an invalid auth type is configured", func() {
		c := &common.Cluster{
			Cluster:    models.Cluster{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(c, auth.AuthType("asdf"))

		Expect(err).To(HaveOccurred())
	})

	It("returns an error for local auth with no private key", func() {
		c := &common.Cluster{
			Cluster:    models.Cluster{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(c, auth.TypeLocal)

		Expect(err).To(HaveOccurred())
	})

	Context("with a private key set", func() {
		var (
			publicKey crypto.PublicKey
		)

		BeforeEach(func() {
			priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			Expect(err).NotTo(HaveOccurred())

			publicKey = priv.Public()

			privBytes, err := x509.MarshalECPrivateKey(priv)
			Expect(err).NotTo(HaveOccurred())

			block := &pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: privBytes,
			}
			var out bytes.Buffer
			Expect(pem.Encode(&out, block)).To(Succeed())

			os.Setenv("EC_PRIVATE_KEY_PEM", out.String())
		})

		AfterEach(func() {
			os.Unsetenv("EC_PRIVATE_KEY_PEM")
		})

		validateToken := func(token string, pub crypto.PublicKey) *jwt.Token {
			parser := &jwt.Parser{ValidMethods: []string{jwt.SigningMethodES256.Alg()}}
			parsed, err := parser.Parse(token, func(t *jwt.Token) (interface{}, error) { return pub, nil })

			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.Valid).To(BeTrue())

			return parsed
		}

		It("creates a valid token", func() {
			c := &common.Cluster{
				Cluster:    models.Cluster{ID: &id},
				PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
			}
			tokenString, err := AgentToken(c, auth.TypeLocal)
			Expect(err).ToNot(HaveOccurred())

			tok := validateToken(tokenString, publicKey)
			claims, ok := tok.Claims.(jwt.MapClaims)
			Expect(ok).To(BeTrue())

			clusterID, ok := claims["cluster_id"].(string)
			Expect(ok).To(BeTrue())
			Expect(clusterID).To(Equal(id.String()))
		})
	})
})
