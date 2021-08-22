package gencrypto

import (
	"crypto"
	"fmt"
	"net/url"
	"os"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("JWT creation", func() {
	It("LocalJWT fails when EC_PRIVATE_KEY_PEM is unset", func() {
		os.Unsetenv("EC_PRIVATE_KEY_PEM")
		_, err := LocalJWT(uuid.New().String(), InfraEnvKey)
		Expect(err).To(HaveOccurred())
	})

	It("LocalJWT fails when EC_PRIVATE_KEY_PEM is empty", func() {
		os.Setenv("EC_PRIVATE_KEY_PEM", "")
		_, err := LocalJWT(uuid.New().String(), InfraEnvKey)
		Expect(err).To(HaveOccurred())
		os.Unsetenv("EC_PRIVATE_KEY_PEM")
	})

	Context("with a private key", func() {
		var (
			publicKey     crypto.PublicKey
			privateKeyPEM string
		)

		BeforeEach(func() {
			var err error
			var publicKeyPEM string
			publicKeyPEM, privateKeyPEM, err = ECDSAKeyPairPEM()
			Expect(err).NotTo(HaveOccurred())
			publicKey, err = jwt.ParseECPublicKeyFromPEM([]byte(publicKeyPEM))
			Expect(err).NotTo(HaveOccurred())
		})

		validateToken := func(token string, pub crypto.PublicKey, id string) {
			parser := &jwt.Parser{ValidMethods: []string{jwt.SigningMethodES256.Alg()}}
			parsed, err := parser.Parse(token, func(t *jwt.Token) (interface{}, error) { return pub, nil })

			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.Valid).To(BeTrue())

			claims, ok := parsed.Claims.(jwt.MapClaims)
			Expect(ok).To(BeTrue())

			clusterID, ok := claims["infra_env_id"].(string)
			Expect(ok).To(BeTrue())
			Expect(clusterID).To(Equal(id))
		}

		Context("with EC_PRIVATE_KEY_PEM set", func() {
			BeforeEach(func() {
				os.Setenv("EC_PRIVATE_KEY_PEM", privateKeyPEM)
			})

			AfterEach(func() {
				os.Unsetenv("EC_PRIVATE_KEY_PEM")
			})

			It("LocalJWT creates a valid token", func() {
				id := uuid.New().String()
				tokenString, err := LocalJWT(id, InfraEnvKey)
				Expect(err).ToNot(HaveOccurred())

				validateToken(tokenString, publicKey, id)
			})

			It("SignURL creates a url with a valid token", func() {
				id := "2dc9400e-1b5e-4e41-bdb5-39b76b006f97"
				u := fmt.Sprintf("https://ai.example.com/api/assisted-install/v1/clusters/%s/downloads/image", id)

				signed, err := SignURL(u, id, InfraEnvKey)
				Expect(err).NotTo(HaveOccurred())
				parsedURL, err := url.Parse(signed)
				Expect(err).NotTo(HaveOccurred())

				q := parsedURL.Query()
				validateToken(q.Get("api_key"), publicKey, id)
			})
		})

		It("LocalJWTForKey creates a valid token", func() {
			id := uuid.New().String()
			tokenString, err := LocalJWTForKey(id, privateKeyPEM, InfraEnvKey)
			Expect(err).ToNot(HaveOccurred())

			validateToken(tokenString, publicKey, id)
		})
	})
})
