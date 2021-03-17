package gencrypto

import (
	"crypto"
	"os"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("JWT creation", func() {
	It("LocalJWT fails when EC_PRIVATE_KEY_PEM is unset", func() {
		os.Unsetenv("EC_PRIVATE_KEY_PEM")
		_, err := LocalJWT(uuid.New().String())
		Expect(err).To(HaveOccurred())
	})

	It("LocalJWT fails when EC_PRIVATE_KEY_PEM is empty", func() {
		os.Setenv("EC_PRIVATE_KEY_PEM", "")
		_, err := LocalJWT(uuid.New().String())
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

		validateToken := func(token string, pub crypto.PublicKey) *jwt.Token {
			parser := &jwt.Parser{ValidMethods: []string{jwt.SigningMethodES256.Alg()}}
			parsed, err := parser.Parse(token, func(t *jwt.Token) (interface{}, error) { return pub, nil })

			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.Valid).To(BeTrue())

			return parsed
		}

		It("LocalJWT creates a valid token with EC_PRIVATE_KEY_PEM set", func() {
			os.Setenv("EC_PRIVATE_KEY_PEM", privateKeyPEM)

			id := uuid.New().String()
			tokenString, err := LocalJWT(id)
			Expect(err).ToNot(HaveOccurred())

			tok := validateToken(tokenString, publicKey)
			claims, ok := tok.Claims.(jwt.MapClaims)
			Expect(ok).To(BeTrue())

			clusterID, ok := claims["cluster_id"].(string)
			Expect(ok).To(BeTrue())
			Expect(clusterID).To(Equal(id))

			os.Unsetenv("EC_PRIVATE_KEY_PEM")
		})

		It("LocalJWTForKey creates a valid token", func() {
			id := uuid.New().String()
			tokenString, err := LocalJWTForKey(id, privateKeyPEM)
			Expect(err).ToNot(HaveOccurred())

			tok := validateToken(tokenString, publicKey)
			claims, ok := tok.Claims.(jwt.MapClaims)
			Expect(ok).To(BeTrue())

			clusterID, ok := claims["cluster_id"].(string)
			Expect(ok).To(BeTrue())
			Expect(clusterID).To(Equal(id))
		})
	})
})
