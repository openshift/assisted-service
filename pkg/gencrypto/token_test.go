package gencrypto

import (
	"crypto"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LocalJWT", func() {
	It("fails when EC_PRIVATE_KEY_PEM is unset", func() {
		os.Unsetenv("EC_PRIVATE_KEY_PEM")
		_, err := LocalJWT(uuid.New().String(), InfraEnvKey)
		Expect(err).To(HaveOccurred())
	})

	It("fails when EC_PRIVATE_KEY_PEM is empty", func() {
		os.Setenv("EC_PRIVATE_KEY_PEM", "")
		_, err := LocalJWT(uuid.New().String(), InfraEnvKey)
		Expect(err).To(HaveOccurred())
		os.Unsetenv("EC_PRIVATE_KEY_PEM")
	})
})

var _ = Context("with an ECDSA key pair", func() {
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

var _ = Describe("JWTForSymmetricKey", func() {
	It("generates a valid jwt", func() {
		key := []byte("1234qwerasdfzxcv")
		token, err := JWTForSymmetricKey(key, 4*time.Hour, "subject")
		Expect(err).ToNot(HaveOccurred())

		parser := &jwt.Parser{ValidMethods: []string{jwt.SigningMethodHS256.Alg()}}
		parsed, err := parser.Parse(token, func(t *jwt.Token) (interface{}, error) { return key, nil })

		Expect(err).ToNot(HaveOccurred())
		Expect(parsed.Valid).To(BeTrue())

		claims, ok := parsed.Claims.(jwt.MapClaims)
		Expect(ok).To(BeTrue())

		_, ok = claims["exp"].(float64)
		Expect(ok).To(BeTrue())

		sub, ok := claims["sub"].(string)
		Expect(ok).To(BeTrue())
		Expect(sub).To(Equal("subject"))
	})
})

var _ = Describe("SignURLWithToken", func() {
	It("adds a url query parameter", func() {
		result, err := SignURLWithToken("https://example.com/things", "api_key", "12345abcde")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("https://example.com/things?api_key=12345abcde"))
	})

	It("adds a url query parameter with existing query parameters", func() {
		result, err := SignURLWithToken("https://example.com/things?key=value&thing=other", "api_key", "12345abcde")
		Expect(err).ToNot(HaveOccurred())

		parsedURL, err := url.Parse(result)
		Expect(err).NotTo(HaveOccurred())

		q := parsedURL.Query()
		Expect(q.Get("api_key")).To(Equal("12345abcde"))
		Expect(q.Get("key")).To(Equal("value"))
		Expect(q.Get("thing")).To(Equal("other"))
	})

	It("fails for an invalid url", func() {
		_, err := SignURLWithToken("https://not a valid url", "api_key", "12345abcde")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ParseExpirationFromURL", func() {
	var url string

	BeforeEach(func() {
		url = "example.com"
	})

	It("successfully parses an expiration from a url with an expiration", func() {
		expiration, err := time.ParseDuration("10h")
		Expect(err).To(BeNil())
		exp := time.Now().Add(expiration).Format("2006-01-02 15:04:05")

		imageTokenKey, err := HMACKey(32)
		Expect(err).To(BeNil())

		token, err := JWTForSymmetricKey([]byte(imageTokenKey), expiration, "test1234")
		Expect(err).To(BeNil())

		urlString, err := SignURLWithToken(url, "image_token", token)
		Expect(err).To(BeNil())

		result, err := ParseExpirationFromURL(urlString)
		Expect(err).To(BeNil())
		Expect(time.Time(*result).Format("2006-01-02 15:04:05")).To(Equal(exp))
	})
	It("fails to parse an expiration from a url with an invalid image_token key", func() {
		urlString, err := SignURLWithToken(url, "image_token", "12345abcde")
		Expect(err).To(BeNil())
		_, err = ParseExpirationFromURL(urlString)
		Expect(err).ToNot(BeNil())
	})
})
