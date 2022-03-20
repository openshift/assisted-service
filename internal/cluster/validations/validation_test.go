package validations

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	auth "github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

// #nosec
const (
	validSecretFormat        = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
	invalidAuthFormat        = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"afsdfasf==\",\"email\":\"r@r.com\"}}}"
	invalidSecretFormat      = "{\"auths\":{\"cloud.openshift.com\":{\"key\":\"abcdef=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"adasfsdf=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"tatastata==\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"afsdfasf==\",\"email\":\"r@r.com\"}}}"
	validSSHPublicKey        = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V1111yr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUA111mn/HalJTHB73LyjilKiPvJ+x2bG7Aeiq111wtQSpt02FCdQGptmsSqqWF/b9botOO38e111PNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= xxxxxx@localhost.localdomain"
	validSSHPublicKeys       = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V1111yr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUA111mn/HalJTHB73LyjilKiPvJ+x2bG7Aeiq111wtQSpt02FCdQGptmsSqqWF/b9botOO38e111PNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= xxxxxx@localhost.localdomain\nssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V1111yr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUA111mn/HalJTHB73LyjilKiPvJ+x2bG7Aeiq111wtQSpt02FCdQGptmsSqqWF/b9botOO38e111PNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= xxxxxx@localhost.localdomain"
	invalidSSHPublicKeyA     = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDI2PBP9RuAHCJ1JvxS0gkK7cm1sMHtdqCYuHzK7fmoMSPeAu+GEPVlBmes825gabO7vUK/pVmcsP9mQLXB0KZ8m/QEBXSO9vmF8dEt5OqtpRLcRzxmcnU1iUs50VSQyEeSxdSV4KA9JuWa+q0f3o3VO+CF6s4kQvQ4lumyCyNSFIBnFCX16+O8syah/UpHUWVqJeHaXCV8qzYKyRvy6nMI5lqCgxe+ENqHkgfkQkgEKHZ8gEnzHtJgewZ3E6fbjQ59eEEvF0zb7WKKWA0YzWOMVGGybj4cFMPQ4Jt7iJ0OZKPBQZMHBcPNrej5lasgcKR7nH5XS0UjHhX5vZJ7e7zONHK4XZj6OjEOXilg3/4rxSn0+QQtT1v0RDXRQhHS6sCyRFV12MqEP8XjPIdBMbE26lRwk3tBwWx7plj3UCVamQid3nY5kslD4X7+cqE8n3bNF922rhCy5STycfEFN3XTs73yKvVPjpro4aQw4BVi4P7B7m7F1d/DqRBuYwWuQ6cLLLLLLLLLLL= root@xxxxxx.xx.xxx.xxx.redhat.com"
	invalidSSHPublicKeyB     = "test!!! ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDi8KHZYGyPQjECHwytquI3rmpgoUn6M+lkeOD2nEKvYElLE5mPIeqF0izJIl56uar2wda+3z107M9QkatE+dP4S9/Ltrlm+/ktAf4O6UoxNLUzv/TGHasb9g3Xkt8JTkohVzVK36622Sd8kLzEc61v1AonLWIADtpwq6/GvHMAuPK2R/H0rdKhTokylKZLDdTqQ+KUFelI6RNIaUBjtVrwkx1j0htxN11DjBVuUyPT2O1ejWegtrM0T+4vXGEA3g3YfbT2k0YnEzjXXqngqbXCYEJCZidp3pJLH/ilo4Y4BId/bx/bhzcbkZPeKlLwjR8g9sydce39bzPIQj+b7nlFv1Vot/77VNwkjXjYPUdUPu0d1PkFD9jKDOdB3fAC61aG2a/8PFS08iBrKiMa48kn+hKXC4G4D5gj/QzIAgzWSl2tEzGQSoIVTucwOAL/jox2dmAa0RyKsnsHORppanuW4qD7KAcmas1GHrAqIfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com"
	userName                 = "jdoe123@example.com"
	validSecretFormatUpdated = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.stage.redhat.io\":{\"auth\":\"c29tZW9uZUBleGFtcGxlLmNvbTp0aGlzaXNhc2VjcmV0\"}}}"
	regCred                  = "someone@example.com:thisisasecret"
)

var _ = Describe("Pull secret validation", func() {

	var secretValidator PullSecretValidator

	log := logrus.New()
	authHandlerDisabled := auth.NewNoneAuthenticator(log.WithField("pkg", "auth"))
	_, JwkCert := auth.GetTokenAndCert(false)
	fakeConfig := &auth.Config{
		JwkCertURL: "",
		JwkCert:    string(JwkCert),
	}
	client := &ocm.Client{
		Authentication: &mockOCMAuthentication{},
		Authorization:  &mockOCMAuthorization{},
		Cache:          cache.New(1*time.Minute, 30*time.Minute),
	}
	authHandler := auth.NewRHSSOAuthenticator(fakeConfig, client, log.WithField("pkg", "auth"), nil)
	Context("test secret format", func() {

		BeforeEach(func() {
			secretValidator, _ = NewPullSecretValidator(Config{})
		})

		It("valid format", func() {
			err := secretValidator.ValidatePullSecret(validSecretFormat, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("empty secret", func() {
			err := secretValidator.ValidatePullSecret("", "", authHandlerDisabled)
			Expect(err).Should(HaveOccurred())
		})
		It("invalid format for the auth", func() {
			err := secretValidator.ValidatePullSecret(invalidAuthFormat, "", authHandlerDisabled)
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
		It("invalid format", func() {
			err := secretValidator.ValidatePullSecret(invalidSecretFormat, "", authHandlerDisabled)
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
		It("valid format - Invalid user", func() {
			err := secretValidator.ValidatePullSecret(validSecretFormat, "NotSameUser@example.com", authHandler)
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
		It("valid format - Valid user", func() {
			err := secretValidator.ValidatePullSecret(validSecretFormat, userName, authHandler)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Add RH Reg PullSecret ", func() {
			ps, err := AddRHRegPullSecret(validSecretFormat, regCred)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ps).To(Equal(validSecretFormatUpdated))
		})
		It("Check empty RH Reg PullSecret ", func() {
			_, err := AddRHRegPullSecret(validSecretFormat, "")
			Expect(err).ShouldNot(BeNil())
		})
	})

	Context("test registries", func() {

		const pullSecDocker = "{\"auths\":{\"docker.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
		const pullSecLegacyDocker = "{\"auths\":{\"https://index.docker.io/v1/\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"

		It("pull secret accepted when it contains all required registries", func() {
			validator, err := NewPullSecretValidator(Config{}, "quay.io/testing:latest", "registry.redhat.io/image:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(validSecretFormat, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret accepted even if it does not contain registry.stage.redhat.io", func() {
			validator, err := NewPullSecretValidator(Config{}, "quay.io/testing:latest", "registry.stage.redhat.io/special:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(validSecretFormat, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret accepted when it doesn't contain auths for ignored registries", func() {
			config := Config{
				PublicRegistries: "ignore.com,something.com",
			}
			validator, err := NewPullSecretValidator(config, "quay.io/testing:latest", "ignore.com/image:v1", "something.com/container:X")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(validSecretFormat, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("docker.io auth is accepted when there is an image from docker.io", func() {
			validator, err := NewPullSecretValidator(Config{}, "docker.io/testing:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(pullSecDocker, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("legacy DockerHub auth is accepted when there is an image from docker.io", func() {
			validator, err := NewPullSecretValidator(Config{}, "docker.io/testing:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(pullSecLegacyDocker, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("docker.io auth is accepted when there is an image with default registry", func() {
			validator, err := NewPullSecretValidator(Config{}, "local:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(pullSecDocker, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("legacy DockerHub auth is accepted when there is an image with default registry", func() {
			validator, err := NewPullSecretValidator(Config{}, "local:v2")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(pullSecLegacyDocker, "", authHandlerDisabled)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("error when pull secret does not contain required registry", func() {
			validator, err := NewPullSecretValidator(Config{}, "quay.io/testing:latest", "required.com/image:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(validSecretFormat, "", authHandlerDisabled)
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
	})
})

var _ = Describe("SSH Key validation", func() {
	It("valid ssh key", func() {
		err := ValidateSSHPublicKey(validSSHPublicKey)
		Expect(err).Should(BeNil())
	})
	It("two valid ssh keys", func() {
		err := ValidateSSHPublicKey(validSSHPublicKeys)
		Expect(err).Should(BeNil())
	})
	It("invalid ssh key", func() {
		var err error
		err = ValidateSSHPublicKey(invalidSSHPublicKeyA)
		Expect(err).ShouldNot(BeNil())
		err = ValidateSSHPublicKey(invalidSSHPublicKeyB)
		Expect(err).ShouldNot(BeNil())
	})
})

type mockOCMAuthentication struct {
	ocm.OCMAuthentication
}

var authenticatePullSecretMock = func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
	payload := &ocm.AuthPayload{}
	payload.Username = userName
	return payload, nil
}

func (m *mockOCMAuthentication) AuthenticatePullSecret(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
	return authenticatePullSecretMock(ctx, pullSecret)
}

type mockOCMAuthorization struct {
	ocm.OCMAuthorization
}

var accessReviewMock func(ctx context.Context, username, action, subscription, resourceType string) (allowed bool, err error)

var capabilityReviewMock = func(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error) {
	return false, nil
}

func (m *mockOCMAuthorization) AccessReview(ctx context.Context, username, action, subscription, resourceType string) (allowed bool, err error) {
	return accessReviewMock(ctx, username, action, subscription, resourceType)
}

func (m *mockOCMAuthorization) CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error) {
	return capabilityReviewMock(ctx, username, capabilityName, capabilityType)
}

var _ = Describe("Cluster name validation", func() {
	It("success", func() {
		err := ValidateClusterNameFormat("test-1")
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("invalid format - special character", func() {
		err := ValidateClusterNameFormat("test!")
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - capital letter", func() {
		err := ValidateClusterNameFormat("testA")
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - starts with number", func() {
		err := ValidateClusterNameFormat("1test")
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - ends with hyphen", func() {
		err := ValidateClusterNameFormat("test-")
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("URL validations", func() {

	Context("test proxy URL", func() {
		var parameters = []struct {
			input, err string
		}{
			{"http://proxy.com:3128", ""},
			{"http://username:pswd@proxy.com", ""},
			{"http://10.9.8.7:123", ""},
			{"http://username:pswd@10.9.8.7:123", ""},
			{
				"https://proxy.com:3128",
				"The URL scheme must be http; https is currently not supported: 'https://proxy.com:3128'",
			},
			{
				"ftp://proxy.com:3128",
				"The URL scheme must be http and specified in the URL: 'ftp://proxy.com:3128'",
			},
			{
				"httpx://proxy.com:3128",
				"Proxy URL format is not valid: 'httpx://proxy.com:3128'",
			},
			{
				"proxy.com:3128",
				"The URL scheme must be http and specified in the URL: 'proxy.com:3128'",
			},
			{
				"xyz",
				"Proxy URL format is not valid: 'xyz'",
			},
			{
				"http",
				"Proxy URL format is not valid: 'http'",
			},
			{
				"",
				"Proxy URL format is not valid: ''",
			},
			{
				"http://!@#$!@#$",
				"Proxy URL format is not valid: 'http://!@#$!@#$'",
			},
		}

		It("validates proxy URL input", func() {
			for _, param := range parameters {
				err := ValidateHTTPProxyFormat(param.input)
				if param.err == "" {
					Expect(err).Should(BeNil())
				} else {
					Expect(err).ShouldNot(BeNil())
					Expect(err.Error()).To(Equal(param.err))
				}
			}
		})
	})

	Context("test URL", func() {
		var parameters = []struct {
			input, err string
		}{
			{"http://ignition.org:3128", ""},
			{"https://ignition.org:3128", ""},
			{"http://ignition.org:3128/config", ""},
			{"https://ignition.org:3128/config", ""},
			{"http://10.9.8.7:123", ""},
			{"http://10.9.8.7:123/config", ""},
			{"", "The URL scheme must be http(s) and specified in the URL: ''"},
			{
				"://!@#$!@#$",
				"URL '://!@#$!@#$' format is not valid: parse \"://!@\": missing protocol scheme",
			},
			{
				"ftp://ignition.com:3128",
				"The URL scheme must be http(s) and specified in the URL: 'ftp://ignition.com:3128'",
			},
			{
				"httpx://ignition.com:3128",
				"The URL scheme must be http(s) and specified in the URL: 'httpx://ignition.com:3128'",
			},
			{
				"ignition.com:3128",
				"The URL scheme must be http(s) and specified in the URL: 'ignition.com:3128'",
			},
		}

		It("validates URL input", func() {
			for _, param := range parameters {
				err := ValidateHTTPFormat(param.input)
				if param.err == "" {
					Expect(err).Should(BeNil())
				} else {
					Expect(err).ShouldNot(BeNil())
					Expect(err.Error()).To(Equal(param.err))
				}
			}
		})
	})

	Context("test no-proxy", func() {
		It("domain name", func() {
			err := ValidateNoProxyFormat("domain.com", "4.7.0")
			Expect(err).Should(BeNil())
		})
		It("domain starts with . for all sub-domains", func() {
			err := ValidateNoProxyFormat(".domain.com", "4.7.0")
			Expect(err).Should(BeNil())
		})
		It("CIDR", func() {
			err := ValidateNoProxyFormat("10.9.0.0/16", "4.7.0")
			Expect(err).Should(BeNil())
		})
		It("IP Address", func() {
			err := ValidateNoProxyFormat("10.9.8.7", "4.7.0")
			Expect(err).Should(BeNil())
		})
		It("multiple entries", func() {
			err := ValidateNoProxyFormat("domain.com,10.9.0.0/16,.otherdomain.com,10.9.8.7", "4.7.0")
			Expect(err).Should(BeNil())
		})
		It("'*' bypass proxy for all destinations FC version", func() {
			err := ValidateNoProxyFormat("*", "4.8.0-fc.7")
			Expect(err).Should(BeNil())
		})
		It("'*' bypass proxy for all destinations release version", func() {
			err := ValidateNoProxyFormat("*", "4.8.0")
			Expect(err).Should(BeNil())
		})
		It("'*' bypass proxy for all destinations not supported pre-4.8.0-fc.4", func() {
			err := ValidateNoProxyFormat("*", "4.7.0")
			Expect(err).ShouldNot(BeNil())
			Expect(err.Error()).Should(ContainSubstring("Sorry, no-proxy value '*' is not supported in this release"))
		})
		It("invalid format", func() {
			err := ValidateNoProxyFormat("...", "4.8.0-fc.7")
			Expect(err).ShouldNot(BeNil())
		})
		It("invalid format of a single value", func() {
			err := ValidateNoProxyFormat("domain.com,...", "4.8.0-fc.7")
			Expect(err).ShouldNot(BeNil())
		})
		It("invalid use of asterisk", func() {
			err := ValidateNoProxyFormat("*,domain.com", "4.8.0-fc.7")
			Expect(err).ShouldNot(BeNil())
		})
	})
})

var _ = Describe("dns name", func() {
	tests := []struct {
		domainName string
		valid      bool
	}{
		{
			domainName: "a.com",
			valid:      true,
		},
		{
			domainName: "a",
			valid:      false,
		},
		{
			domainName: "co",
			valid:      false,
		},
		{
			domainName: "aaa",
			valid:      false,
		},
		{
			domainName: "abc.def",
			valid:      true,
		},
		{
			domainName: "-aaa.com",
			valid:      false,
		},
		{
			domainName: "a-aa.com",
			valid:      true,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("Domain name \"%s\"", t.domainName), func() {
			if t.valid {
				Expect(ValidateDomainNameFormat(t.domainName)).ToNot(HaveOccurred())
			} else {
				Expect(ValidateDomainNameFormat(t.domainName)).To(HaveOccurred())
			}
		})
	}
})

var _ = Describe("Get registry from container image name", func() {

	It("all registries present when ignore list empty", func() {
		images := []string{"registry.redhat.io/fedora:32", "quay.io/ocpmetal/assisted-service:latest"}
		registries, err := getRegistriesWithAuth("", ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(2))
		Expect(*registries).Should(HaveKey("registry.redhat.io"))
		Expect(*registries).Should(HaveKey("quay.io"))
	})

	It("multiple images with same registry result in one auth entry", func() {
		images := []string{"quay.io/ocpmetal/assisted-service:4.6", "quay.io/ocpmetal/assisted-service:latest"}
		registries, err := getRegistriesWithAuth("", ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey("quay.io"))
	})

	It("port preserved in image registry", func() {
		images := []string{"localhost:5000/private/service:v1"}
		registries, err := getRegistriesWithAuth("", ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey("localhost:5000"))
	})

	It("empty registry is replaced with official docker registry", func() {
		images := []string{"private/service:v1"}
		registries, err := getRegistriesWithAuth("", ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey(dockerHubRegistry))
	})

	It("registries omitted when in ignore list with comma (,) separator", func() {
		images := []string{"quay.io/private/service:latest", "localhost:5050/private/service:v1", "registry.redhat.io/fedora:32"}
		registries, err := getRegistriesWithAuth("quay.io,localhost:5050", ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey("registry.redhat.io"))
	})

	It("registries omitted when in ignore list with semicolon (;) separator", func() {
		images := []string{"quay.io/private/service:latest", "localhost:5050/private/service:v1", "registry.redhat.io/fedora:32"}
		registries, err := getRegistriesWithAuth("quay.io;localhost:5050", ";", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey("registry.redhat.io"))
	})

	It("all multiple entries from the same registries omitted when in ingore list", func() {
		images := []string{"quay.io/private/service:v1", "quay.io/ocpmetal/assisted-service:latest"}
		registries, err := getRegistriesWithAuth("quay.io", ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("docker official registry is ignored when in ignore list", func() {
		images := []string{dockerHubRegistry + "/private/service:v1"}
		registries, err := getRegistriesWithAuth(dockerHubRegistry, ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("docker official registry is ignored when docker legacy URL in ignore list", func() {
		images := []string{dockerHubRegistry + "/private/service:v1"}
		registries, err := getRegistriesWithAuth(dockerHubLegacyAuth, ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("default registry is ignored when docker official registry in ignore list", func() {
		images := []string{"private/service:v1"}
		registries, err := getRegistriesWithAuth(dockerHubRegistry, ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("default registry is ignored when docker registry URL in ignore list", func() {
		images := []string{"private/service:v1"}
		registries, err := getRegistriesWithAuth(dockerHubLegacyAuth, ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("registries list empty when images empty", func() {
		images := []string{}
		registries, err := getRegistriesWithAuth("", ",", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("nothing ignored when ignore list uses wrong separator", func() {
		images := []string{"quay.io/private/service:latest", "localhost:5050/private/service:v1", "registry.redhat.io/fedora:32"}
		registries, err := getRegistriesWithAuth("quay.io,localhost:5050", ";", images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(3))
		Expect(*registries).Should(HaveKey("registry.redhat.io"))
		Expect(*registries).Should(HaveKey("quay.io"))
		Expect(*registries).Should(HaveKey("localhost:5050"))
	})

	It("error occurs when image list contains malformed image name", func() {
		images := []string{"quay.io:X/private/service:latest"}
		_, err := getRegistriesWithAuth("", ";", images...)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("NTP source", func() {
	tests := []struct {
		ntpSource string
		valid     bool
	}{
		{
			ntpSource: "1.1.1.1",
			valid:     true,
		},
		{
			ntpSource: "clock.redhat.com",
			valid:     true,
		},
		{
			ntpSource: "alias",
			valid:     true,
		},
		{
			ntpSource: "comma,separated,list",
			valid:     true,
		},
		{
			ntpSource: "!jkfd.com",
			valid:     false,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("NTP source \"%s\"", t.ntpSource), func() {
			if t.valid {
				Expect(ValidateAdditionalNTPSource(t.ntpSource)).To(BeTrue())
			} else {
				Expect(ValidateAdditionalNTPSource(t.ntpSource)).To(BeFalse())
			}
		})
	}
})

var _ = Describe("vip dhcp allocation", func() {
	tests := []struct {
		vipDHCPAllocation  bool
		machineNetworkCIDR string
		valid              bool
	}{
		{
			vipDHCPAllocation:  true,
			machineNetworkCIDR: "1001:db8::/120",
			valid:              false,
		},
		{
			vipDHCPAllocation:  false,
			machineNetworkCIDR: "1001:db8::/120",
			valid:              true,
		},
		{
			vipDHCPAllocation:  true,
			machineNetworkCIDR: "10.56.20.0/24",
			valid:              true,
		},
		{
			vipDHCPAllocation:  false,
			machineNetworkCIDR: "10.56.20.0/24",
			valid:              true,
		},
		{
			vipDHCPAllocation:  true,
			machineNetworkCIDR: "",
			valid:              true,
		},
		{
			vipDHCPAllocation:  false,
			machineNetworkCIDR: "",
			valid:              true,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("VIP DHCP allocation: %t, machine network: %s", t.vipDHCPAllocation, t.machineNetworkCIDR), func() {
			if t.valid {
				Expect(ValidateVipDHCPAllocationWithIPv6(t.vipDHCPAllocation, t.machineNetworkCIDR)).ToNot(HaveOccurred())
			} else {
				Expect(ValidateVipDHCPAllocationWithIPv6(t.vipDHCPAllocation, t.machineNetworkCIDR)).To(HaveOccurred())
			}
		})
	}
})

var _ = Describe("IPv6 support", func() {
	tests := []struct {
		ipV6Supported bool
		element       []*string
		valid         bool
	}{
		{
			ipV6Supported: true,
			element:       []*string{swag.String("1001:db8::/120")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("1001:db8::/120")},
			valid:         false,
		},
		{
			ipV6Supported: true,
			element:       []*string{swag.String("10.56.20.0/24")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("10.56.20.0/24")},
			valid:         true,
		},
		{
			ipV6Supported: true,
			element:       []*string{swag.String("1001:db8::1")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("1001:db8::1")},
			valid:         false,
		},
		{
			ipV6Supported: true,
			element:       []*string{swag.String("10.56.20.70")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("10.56.20.70")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{nil},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{nil, swag.String("1001:db8::1")},
			valid:         false,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("10.56.20.70"), swag.String("1001:db8::1")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("1001:db8::/64"), swag.String("10.56.20.0/24")},
			valid:         true,
		},
		{
			ipV6Supported: false,
			element:       []*string{swag.String("10.56.20.70"), swag.String("10.56.20.0/24")},
			valid:         true,
		},
		{
			ipV6Supported: true,
			element:       []*string{swag.String("10.56.20.70"), swag.String("1001:db8::1")},
			valid:         true,
		},
		{
			ipV6Supported: true,
			element:       []*string{swag.String("1001:db8::1"), swag.String("10.56.20.70")},
			valid:         true,
		},
	}
	for _, t := range tests {
		t := t
		It(fmt.Sprintf("IPv6 support validation. Supported: %t, IP addresses/CIDRs: %v", t.ipV6Supported, t.element), func() {
			if t.valid {
				Expect(ValidateIPAddressFamily(t.ipV6Supported, t.element...)).ToNot(HaveOccurred())
			} else {
				Expect(ValidateIPAddressFamily(t.ipV6Supported, t.element...)).To(HaveOccurred())
			}
		})
	}
})

var _ = Describe("Machine Network amount and order", func() {
	tests := []struct {
		element []*models.MachineNetwork
		valid   bool
	}{
		{
			element: []*models.MachineNetwork{{Cidr: "1.2.5.0/24"}, {Cidr: "1002:db8::/119"}},
			valid:   true,
		},
		{
			// Invalid because violates the "IPv4 subnet as the first one" constraint
			element: []*models.MachineNetwork{{Cidr: "1002:db8::/119"}, {Cidr: "1.2.5.0/24"}},
			valid:   false,
		},
		{
			// Invalid because violates the "exactly 2 networks" constraint
			element: []*models.MachineNetwork{{Cidr: "1.2.5.0/24"}, {Cidr: "1002:db8::/119"}, {Cidr: "1.2.6.0/24"}, {Cidr: "1.2.7.0/24"}},
			valid:   false,
		},
		{
			// Invalid because violates the "exactly 2 networks" constraint
			element: []*models.MachineNetwork{{Cidr: "1002:db8::/119"}, {Cidr: "1.2.5.0/24"}, {Cidr: "1.2.6.0/24"}, {Cidr: "1.2.7.0/24"}},
			valid:   false,
		},
	}
	for _, test := range tests {
		t := test
		It(fmt.Sprintf("Dual-stack machine network order validation. IP addresses/CIDRs: %v", t.element), func() {
			if t.valid {
				Expect(network.VerifyMachineNetworksDualStack(t.element, true)).ToNot(HaveOccurred())
			} else {
				Expect(network.VerifyMachineNetworksDualStack(t.element, true)).To(HaveOccurred())
			}
		})
	}
})

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster validations tests")
}
