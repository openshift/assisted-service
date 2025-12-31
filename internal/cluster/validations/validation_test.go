package validations

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	auth "github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/testutil"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

// #nosec
const (
	// dXNlcjpwYXNzd29yZAo= <-> user:password
	// dXNlcjpwYXNzOndvcmQK <-> user:pass:word
	validPullSecretWithCIToken = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}, \"registry.ci\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
	validSecretFormat          = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzOndvcmQK\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
	validSecretWithRepo        = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io/testing\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}, \"registry.ci\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
	invalidAuthFormat          = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"afsdfasf==\",\"email\":\"r@r.com\"}}}"
	invalidSecretFormat        = "{\"auths\":{\"cloud.openshift.com\":{\"key\":\"abcdef=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"adasfsdf=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"tatastata==\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"afsdfasf==\",\"email\":\"r@r.com\"}}}"
	invalidStrSecretFormat     = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":null,\"email\":null},\"quay.io\":{\"auth\":\"adasfsdf=\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"tatastata==\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"afsdfasf==\",\"email\":\"r@r.com\"}}}"
	validSSHPublicKey          = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V1111yr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUA111mn/HalJTHB73LyjilKiPvJ+x2bG7Aeiq111wtQSpt02FCdQGptmsSqqWF/b9botOO38e111PNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= xxxxxx@localhost.localdomain"
	validSSHPublicKeys         = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V1111yr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUA111mn/HalJTHB73LyjilKiPvJ+x2bG7Aeiq111wtQSpt02FCdQGptmsSqqWF/b9botOO38e111PNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= xxxxxx@localhost.localdomain\nssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V1111yr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUA111mn/HalJTHB73LyjilKiPvJ+x2bG7Aeiq111wtQSpt02FCdQGptmsSqqWF/b9botOO38e111PNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= xxxxxx@localhost.localdomain"
	invalidSSHPublicKeyA       = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDI2PBP9RuAHCJ1JvxS0gkK7cm1sMHtdqCYuHzK7fmoMSPeAu+GEPVlBmes825gabO7vUK/pVmcsP9mQLXB0KZ8m/QEBXSO9vmF8dEt5OqtpRLcRzxmcnU1iUs50VSQyEeSxdSV4KA9JuWa+q0f3o3VO+CF6s4kQvQ4lumyCyNSFIBnFCX16+O8syah/UpHUWVqJeHaXCV8qzYKyRvy6nMI5lqCgxe+ENqHkgfkQkgEKHZ8gEnzHtJgewZ3E6fbjQ59eEEvF0zb7WKKWA0YzWOMVGGybj4cFMPQ4Jt7iJ0OZKPBQZMHBcPNrej5lasgcKR7nH5XS0UjHhX5vZJ7e7zONHK4XZj6OjEOXilg3/4rxSn0+QQtT1v0RDXRQhHS6sCyRFV12MqEP8XjPIdBMbE26lRwk3tBwWx7plj3UCVamQid3nY5kslD4X7+cqE8n3bNF922rhCy5STycfEFN3XTs73yKvVPjpro4aQw4BVi4P7B7m7F1d/DqRBuYwWuQ6cLLLLLLLLLLL= root@xxxxxx.xx.xxx.xxx.redhat.com"
	invalidSSHPublicKeyB       = "test!!! ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDi8KHZYGyPQjECHwytquI3rmpgoUn6M+lkeOD2nEKvYElLE5mPIeqF0izJIl56uar2wda+3z107M9QkatE+dP4S9/Ltrlm+/ktAf4O6UoxNLUzv/TGHasb9g3Xkt8JTkohVzVK36622Sd8kLzEc61v1AonLWIADtpwq6/GvHMAuPK2R/H0rdKhTokylKZLDdTqQ+KUFelI6RNIaUBjtVrwkx1j0htxN11DjBVuUyPT2O1ejWegtrM0T+4vXGEA3g3YfbT2k0YnEzjXXqngqbXCYEJCZidp3pJLH/ilo4Y4BId/bx/bhzcbkZPeKlLwjR8g9sydce39bzPIQj+b7nlFv1Vot/77VNwkjXjYPUdUPu0d1PkFD9jKDOdB3fAC61aG2a/8PFS08iBrKiMa48kn+hKXC4G4D5gj/QzIAgzWSl2tEzGQSoIVTucwOAL/jox2dmAa0RyKsnsHORppanuW4qD7KAcmas1GHrAqIfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com"
	userName                   = "jdoe123@example.com"
	validSecretFormatUpdated   = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"quay.io\":{\"auth\":\"dXNlcjpwYXNzOndvcmQK\",\"email\":\"r@r.com\"},\"registry.connect.redhat.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.redhat.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"},\"registry.stage.redhat.io\":{\"auth\":\"c29tZW9uZUBleGFtcGxlLmNvbTp0aGlzaXNhc2VjcmV0\"}}}"
	regCred                    = "someone@example.com:thisisasecret"
)

var _ = Describe("Pull secret validation", func() {

	var secretValidator PullSecretValidator
	var secretValidatorWithNoAuth PullSecretValidator
	var additionalIgnoredRegistries []string

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
			secretValidator, _ = NewPullSecretValidator(map[string]bool{}, authHandler)
			secretValidatorWithNoAuth, _ = NewPullSecretValidator(map[string]bool{}, authHandlerDisabled)
		})

		It("valid format", func() {
			err := secretValidatorWithNoAuth.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("empty secret", func() {
			err := secretValidatorWithNoAuth.ValidatePullSecret(additionalIgnoredRegistries, "", "", "")
			Expect(err).Should(HaveOccurred())
		})
		It("invalid format for the auth", func() {
			err := secretValidatorWithNoAuth.ValidatePullSecret(additionalIgnoredRegistries, invalidAuthFormat, "", "")
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
		It("invalid format", func() {
			err := secretValidatorWithNoAuth.ValidatePullSecret(additionalIgnoredRegistries, invalidSecretFormat, "", "")
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
		It("invalid format - non-string", func() {
			err := secretValidatorWithNoAuth.ValidatePullSecret(additionalIgnoredRegistries, invalidStrSecretFormat, "", "")
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
		It("valid format - Invalid user", func() {
			err := secretValidator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "NotSameUser@example.com", "")
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(BeAssignableToTypeOf(&PullSecretError{}))
		})
		It("valid format - Valid user", func() {
			err := secretValidator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, userName, "")
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
		var additionalIgnoredRegistries []string
		const pullSecDocker = "{\"auths\":{\"docker.io\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
		const pullSecLegacyDocker = "{\"auths\":{\"https://index.docker.io/v1/\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"

		It("pull secret accepted when it contains all required registries", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "quay.io/testing:latest", "registry.redhat.io/image:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret accepted even if it does not contain registry.stage.redhat.io", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "quay.io/testing:latest", "registry.stage.redhat.io/special:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret accepted when it doesn't contain auths for ignored registries", func() {
			publicRegistries := map[string]bool{
				"ignore.com":    true,
				"something.com": true,
			}
			validator, err := NewPullSecretValidator(publicRegistries, authHandlerDisabled, "quay.io/testing:latest", "ignore.com/image:v1", "something.com/container:X")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret accepted when it doesn't contain auths for additional ignored registries", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "quay.io/testing:latest", "ignore.com/image:v1")
			Expect(err).ShouldNot(HaveOccurred())
			additionalIgnoredRegistries = []string{"ignore.com"}
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret accepted when release image is specified and its registry credentials exists", func() {
			publicRegistries := map[string]bool{}
			validator, err := NewPullSecretValidator(publicRegistries, authHandlerDisabled, "quay.io/testing:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validPullSecretWithCIToken, "", "registry.ci/test:latest")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret accepted when release image is specified and the full path for the registry credentials exists", func() {
			publicRegistries := map[string]bool{}
			validator, err := NewPullSecretValidator(publicRegistries, authHandlerDisabled, "quay.io/testing/image-name:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretWithRepo, "", "registry.ci/test:latest")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("pull secret is not accepted when release image specified is in a different repo than the registry credentials", func() {
			publicRegistries := map[string]bool{}
			validator, err := NewPullSecretValidator(publicRegistries, authHandlerDisabled, "quay.io/not-testing/image-name:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretWithRepo, "", "registry.ci/test:latest")
			Expect(err).Should(HaveOccurred())
		})

		It("pull secret is not accepted when release image is specified but its registry credentials missing", func() {
			publicRegistries := map[string]bool{}
			validator, err := NewPullSecretValidator(publicRegistries, authHandlerDisabled, "quay.io/testing:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "", "registry.ci/test:latest")
			Expect(err).Should(HaveOccurred())
		})

		It("docker.io auth is accepted when there is an image from docker.io", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "docker.io/testing:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, pullSecDocker, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("legacy DockerHub auth is accepted when there is an image from docker.io", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "docker.io/testing:latest")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, pullSecLegacyDocker, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("docker.io auth is accepted when there is an image with default registry", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "local:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, pullSecDocker, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("legacy DockerHub auth is accepted when there is an image with default registry", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "local:v2")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, pullSecLegacyDocker, "", "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("error when pull secret does not contain required registry", func() {
			validator, err := NewPullSecretValidator(map[string]bool{}, authHandlerDisabled, "quay.io/testing:latest", "required.com/image:v1")
			Expect(err).ShouldNot(HaveOccurred())
			err = validator.ValidatePullSecret(additionalIgnoredRegistries, validSecretFormat, "", "")
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
	const (
		bareMetalPlatform = string(models.PlatformTypeBaremetal)
		nonePlatform      = string(models.PlatformTypeNone)
	)
	It("valid", func() {
		err := ValidateClusterNameFormat("test-1", bareMetalPlatform)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("valid - starts with number", func() {
		err := ValidateClusterNameFormat("1-test", bareMetalPlatform)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("valid - contains a period and None platform", func() {
		err := ValidateClusterNameFormat("test.test", nonePlatform)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("invalid format - contains a period and not None platform", func() {
		err := ValidateClusterNameFormat("test.test", bareMetalPlatform)
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - special character", func() {
		err := ValidateClusterNameFormat("test!", bareMetalPlatform)
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - capital letter", func() {
		err := ValidateClusterNameFormat("testA", bareMetalPlatform)
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - starts with capital letter", func() {
		err := ValidateClusterNameFormat("Test", bareMetalPlatform)
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - ends with hyphen", func() {
		err := ValidateClusterNameFormat("test-", bareMetalPlatform)
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - starts with hyphen", func() {
		err := ValidateClusterNameFormat("-test", bareMetalPlatform)
		Expect(err).Should(HaveOccurred())
	})
	It("invalid format - starts with a period", func() {
		err := ValidateClusterNameFormat(".test", bareMetalPlatform)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("URL validations", func() {

	Context("test no-proxy", func() {

		Context("test no-proxy with ocpVersion 4.7.0", func() {
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
			It("'*' bypass proxy for all destinations not supported pre-4.8.0-fc.4", func() {
				err := ValidateNoProxyFormat("*", "4.7.0")
				Expect(err).ShouldNot(BeNil())
				Expect(err.Error()).Should(ContainSubstring("Sorry, no-proxy value '*' is not supported in release: 4.7.0"))
			})
			It("'*,domain.com' bypass proxy for all destinations not supported pre-4.8.0-fc.4", func() {
				err := ValidateNoProxyFormat("*,domain.com", "4.7.0")
				Expect(err).ShouldNot(BeNil())
				Expect(err.Error()).Should(ContainSubstring("Sorry, no-proxy value '*' is not supported in release: 4.7.0"))
			})
		})
		Context("test no-proxy with ocpVersion 4.8.0", func() {
			It("'*' bypass proxy for all destinations FC version", func() {
				err := ValidateNoProxyFormat("*", "4.8.0-fc.7")
				Expect(err).Should(BeNil())
			})
			It("'*' bypass proxy for all destinations release version", func() {
				err := ValidateNoProxyFormat("*", "4.8.0")
				Expect(err).Should(BeNil())
			})
			It("invalid format", func() {
				err := ValidateNoProxyFormat("...", "4.8.0-fc.7")
				Expect(err).ShouldNot(BeNil())
			})
			It("invalid format of a single value", func() {
				err := ValidateNoProxyFormat("domain.com,...", "4.8.0-fc.7")
				Expect(err).ShouldNot(BeNil())
			})
			It("A use of asterisk", func() {
				err := ValidateNoProxyFormat("*,domain.com", "4.8.0-fc.7")
				Expect(err).Should(BeNil())
			})
		})
		Context("test no-proxy with no ocpVersion (InfraEnv)", func() {
			It("'*' bypass proxy for all destinations FC version", func() {
				err := ValidateNoProxyFormat("*", "")
				Expect(err).Should(BeNil())
			})
			It("'*' bypass proxy for all destinations release version", func() {
				err := ValidateNoProxyFormat("*", "")
				Expect(err).Should(BeNil())
			})
			It("invalid format", func() {
				err := ValidateNoProxyFormat("...", "")
				Expect(err).ShouldNot(BeNil())
			})
			It("invalid format of a single value", func() {
				err := ValidateNoProxyFormat("domain.com,...", "")
				Expect(err).ShouldNot(BeNil())
			})
			It("A use of asterisk", func() {
				err := ValidateNoProxyFormat("*,domain.com", "")
				Expect(err).Should(BeNil())
			})
		})
	})
})

var _ = Describe("Get registries", func() {
	getRegistriesWithAuth := func(ignorableImages map[string]bool, images ...string) (*map[string]bool, error) {
		registriesWithAuth := map[string]bool{}
		for _, image := range images {
			registry, err := getRegistryAuthStatus(ignorableImages, image)
			if err != nil {
				return nil, err
			}

			if registry != nil {
				registriesWithAuth[*registry] = true
			}
		}

		return &registriesWithAuth, nil
	}

	var ignorableImages map[string]bool

	BeforeEach(func() {
		ignorableImages = map[string]bool{}
	})

	It("all registries present when ignore list empty", func() {
		images := []string{"registry.redhat.io/fedora:32", "quay.io/example/assisted-service:latest"}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(2))
		Expect(*registries).Should(HaveKey("registry.redhat.io/fedora"))
		Expect(*registries).Should(HaveKey("quay.io/example/assisted-service"))
	})

	It("multiple images with same registry result in one auth entry", func() {
		images := []string{"quay.io/example/assisted-service:4.6", "quay.io/example/assisted-service:latest"}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey("quay.io/example/assisted-service"))
	})

	It("port preserved in image registry", func() {
		images := []string{"localhost:5000/private/service:v1"}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey("localhost:5000/private/service"))
	})

	It("empty registry is replaced with official docker registry", func() {
		images := []string{"private/service:v1"}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey(fmt.Sprintf("%s/private/service", dockerHubRegistry)))
	})

	It("registries omitted when in ignore list with comma (,) separator", func() {
		images := []string{"quay.io/private/service:latest", "localhost:5050/private/service:v1", "registry.redhat.io/fedora:32"}
		ignorableImages = map[string]bool{
			"quay.io":        true,
			"localhost:5050": true,
		}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(1))
		Expect(*registries).Should(HaveKey("registry.redhat.io/fedora"))
	})

	It("all multiple entries from the same registries omitted when in ingore list", func() {
		images := []string{"quay.io/private/service:v1", "quay.io/example/assisted-service:latest"}
		ignorableImages = map[string]bool{
			"quay.io": true,
		}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("docker official registry is ignored when in ignore list", func() {
		images := []string{dockerHubRegistry + "/private/service:v1"}
		ignorableImages = map[string]bool{
			dockerHubRegistry: true,
		}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("docker official registry is ignored when docker legacy URL in ignore list", func() {
		images := []string{dockerHubRegistry + "/private/service:v1"}
		ignorableImages = map[string]bool{
			dockerHubLegacyAuth: true,
		}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("default registry is ignored when docker official registry in ignore list", func() {
		images := []string{"private/service:v1"}
		ignorableImages = map[string]bool{
			dockerHubRegistry: true,
		}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("default registry is ignored when docker registry URL in ignore list", func() {
		images := []string{"private/service:v1"}
		ignorableImages = map[string]bool{
			dockerHubLegacyAuth: true,
		}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("registries list empty when images empty", func() {
		images := []string{}
		registries, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*registries).Should(HaveLen(0))
	})

	It("error occurs when image list contains malformed image name", func() {
		images := []string{"quay.io:X/private/service:latest"}
		_, err := getRegistriesWithAuth(ignorableImages, images...)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("VIP Dual-Stack Validation", func() {
	var cluster common.Cluster

	BeforeEach(func() {
		cluster = common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.12.0",
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
					{Cidr: "2001:db8::/32"},
				},
			},
		}
	})

	Describe("validateVIPAddressFamily - Dual Stack API VIPs", func() {
		Context("OCP 4.12+ (IPv6-primary allowed)", func() {
			It("accepts IPv4-first, IPv6-second API VIPs", func() {
				cluster.APIVips = []*models.APIVip{
					{IP: "192.168.1.100"},
					{IP: "2001:db8::100"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(2))
				Expect(*errors[0]).To(Equal("192.168.1.100"))
				Expect(*errors[1]).To(Equal("2001:db8::100"))
			})

			It("accepts IPv6-first, IPv4-second API VIPs", func() {
				cluster.APIVips = []*models.APIVip{
					{IP: "2001:db8::100"},
					{IP: "192.168.1.100"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(2))
				Expect(*errors[0]).To(Equal("2001:db8::100"))
				Expect(*errors[1]).To(Equal("192.168.1.100"))
			})

			It("accepts single IPv4 API VIP in dual-stack cluster", func() {
				cluster.APIVips = []*models.APIVip{
					{IP: "192.168.1.100"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(1))
				Expect(*errors[0]).To(Equal("192.168.1.100"))
			})

			It("accepts single IPv6 API VIP in dual-stack cluster", func() {
				cluster.APIVips = []*models.APIVip{
					{IP: "2001:db8::100"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(1))
				Expect(*errors[0]).To(Equal("2001:db8::100"))
			})
		})

		Context("OCP 4.11 and below (IPv4-primary only)", func() {
			BeforeEach(func() {
				cluster.OpenshiftVersion = "4.11.0"
			})

			It("rejects IPv6-first API VIPs", func() {
				cluster.APIVips = []*models.APIVip{
					{IP: "2001:db8::100"},
					{IP: "192.168.1.100"},
				}
				errors, err := validateVIPAddressFamily(false, cluster)
				Expect(err).ToNot(BeNil())
				Expect(errors).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("IPv6-primary dual-stack requires OpenShift 4.12+"))
			})

			It("accepts IPv4-first API VIPs", func() {
				cluster.APIVips = []*models.APIVip{
					{IP: "192.168.1.100"},
					{IP: "2001:db8::100"},
				}
				errors, err := validateVIPAddressFamily(false, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(2))
				Expect(*errors[0]).To(Equal("192.168.1.100"))
				Expect(*errors[1]).To(Equal("2001:db8::100"))
			})
		})
	})

	Describe("validateVIPAddressFamily - Dual Stack Ingress VIPs", func() {
		Context("OCP 4.12+ (IPv6-primary allowed)", func() {
			It("accepts IPv4-first, IPv6-second Ingress VIPs", func() {
				cluster.IngressVips = []*models.IngressVip{
					{IP: "192.168.1.101"},
					{IP: "2001:db8::101"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(2))
				Expect(*errors[0]).To(Equal("192.168.1.101"))
				Expect(*errors[1]).To(Equal("2001:db8::101"))
			})

			It("accepts IPv6-first, IPv4-second Ingress VIPs", func() {
				cluster.IngressVips = []*models.IngressVip{
					{IP: "2001:db8::101"},
					{IP: "192.168.1.101"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(2))
				Expect(*errors[0]).To(Equal("2001:db8::101"))
				Expect(*errors[1]).To(Equal("192.168.1.101"))
			})

			It("accepts single IPv4 Ingress VIP in dual-stack cluster", func() {
				cluster.IngressVips = []*models.IngressVip{
					{IP: "192.168.1.101"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(1))
				Expect(*errors[0]).To(Equal("192.168.1.101"))
			})

			It("accepts single IPv6 Ingress VIP in dual-stack cluster", func() {
				cluster.IngressVips = []*models.IngressVip{
					{IP: "2001:db8::101"},
				}
				errors, err := validateVIPAddressFamily(true, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(1))
				Expect(*errors[0]).To(Equal("2001:db8::101"))
			})
		})

		Context("OCP 4.11 and below (IPv4-primary only)", func() {
			BeforeEach(func() {
				cluster.OpenshiftVersion = "4.11.0"
			})

			It("rejects IPv6-first Ingress VIPs", func() {
				cluster.IngressVips = []*models.IngressVip{
					{IP: "2001:db8::101"},
					{IP: "192.168.1.101"},
				}
				errors, err := validateVIPAddressFamily(false, cluster)
				Expect(err).ToNot(BeNil())
				Expect(errors).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("IPv6-primary dual-stack requires OpenShift 4.12+"))
			})

			It("accepts IPv4-first Ingress VIPs", func() {
				cluster.IngressVips = []*models.IngressVip{
					{IP: "192.168.1.101"},
					{IP: "2001:db8::101"},
				}
				errors, err := validateVIPAddressFamily(false, cluster)
				Expect(err).To(BeNil())
				Expect(errors).To(HaveLen(2))
				Expect(*errors[0]).To(Equal("192.168.1.101"))
				Expect(*errors[1]).To(Equal("2001:db8::101"))
			})
		})
	})

	Describe("validateVIPAddressFamily - Mixed VIP types", func() {
		It("validates both API and Ingress VIPs together", func() {
			cluster.APIVips = []*models.APIVip{
				{IP: "192.168.1.100"},
				{IP: "2001:db8::100"},
			}
			cluster.IngressVips = []*models.IngressVip{
				{IP: "192.168.1.101"},
				{IP: "2001:db8::101"},
			}
			errors, err := validateVIPAddressFamily(true, cluster)
			Expect(err).To(BeNil())
			Expect(errors).To(HaveLen(4))
			Expect(*errors[0]).To(Equal("192.168.1.100"))
			Expect(*errors[1]).To(Equal("2001:db8::100"))
			Expect(*errors[2]).To(Equal("192.168.1.101"))
			Expect(*errors[3]).To(Equal("2001:db8::101"))
		})

		It("accepts mixed IPv4-first and IPv6-first VIPs (each type validated independently)", func() {
			cluster.APIVips = []*models.APIVip{
				{IP: "192.168.1.100"},
				{IP: "2001:db8::100"},
			}
			cluster.IngressVips = []*models.IngressVip{
				{IP: "2001:db8::101"},
				{IP: "192.168.1.101"},
			}
			errors, err := validateVIPAddressFamily(true, cluster)
			Expect(err).To(BeNil())
			Expect(errors).To(HaveLen(4))
			Expect(*errors[0]).To(Equal("192.168.1.100"))
			Expect(*errors[1]).To(Equal("2001:db8::100"))
			Expect(*errors[2]).To(Equal("2001:db8::101"))
			Expect(*errors[3]).To(Equal("192.168.1.101"))
		})
	})
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
	v6 := common.PrimaryIPStackV6
	v4 := common.PrimaryIPStackV4

	tests := []struct {
		ipV6Supported  bool
		element        []*string
		valid          bool
		primaryIPStack *common.PrimaryIPStack
	}{
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("1001:db8::/120")},
			primaryIPStack: &v6,
			valid:          true,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("1001:db8::/120")},
			primaryIPStack: nil,
			valid:          true,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("1001:db8::/120")},
			valid:          false,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("1001:db8::/120")},
			valid:          true,
			primaryIPStack: &v6,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("10.56.20.0/24")},
			valid:          true,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("10.56.20.0/24")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("10.56.20.0/24")},
			valid:          true,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("10.56.20.0/24")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("1001:db8::1")},
			valid:          true,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("1001:db8::1")},
			valid:          true,
			primaryIPStack: &v6,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("1001:db8::1")},
			valid:          false,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("1001:db8::1")},
			valid:          true,
			primaryIPStack: &v6,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("10.56.20.70")},
			valid:          true,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("10.56.20.70")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("10.56.20.70")},
			valid:          true,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("10.56.20.70")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("")},
			valid:          true,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("")},
			valid:          true,
			primaryIPStack: &v6,
		},
		{
			ipV6Supported:  false,
			element:        []*string{nil},
			valid:          true,
			primaryIPStack: nil,
		},
		{
			ipV6Supported:  false,
			element:        []*string{nil},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  false,
			element:        []*string{nil},
			valid:          true,
			primaryIPStack: &v6,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("10.56.20.70"), swag.String("1001:db8::1")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("1001:db8::/64"), swag.String("10.56.20.0/24")},
			valid:          true,
			primaryIPStack: &v6,
		},
		{
			ipV6Supported:  false,
			element:        []*string{swag.String("10.56.20.70"), swag.String("10.56.20.0/24")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("10.56.20.70"), swag.String("1001:db8::1")},
			valid:          true,
			primaryIPStack: &v4,
		},
		{
			ipV6Supported:  true,
			element:        []*string{swag.String("1001:db8::1"), swag.String("10.56.20.70")},
			valid:          true,
			primaryIPStack: &v6,
		},
	}
	for _, t := range tests {
		It(fmt.Sprintf("IPv6 support validation. Supported: %t, IP addresses/CIDRs: %v", t.ipV6Supported, t.element), func() {
			if t.valid {
				Expect(ValidateIPAddressFamily(t.ipV6Supported, "networks", t.primaryIPStack, t.element...)).ToNot(HaveOccurred())
			} else {
				Expect(ValidateIPAddressFamily(t.ipV6Supported, "networks", t.primaryIPStack, t.element...)).To(HaveOccurred())
			}
		})
	}
})

var _ = Describe("Machine Network amount and order", func() {
	tests := []struct {
		element          []*models.MachineNetwork
		valid            bool
		description      string
		openshiftVersion string
	}{
		{
			element:          []*models.MachineNetwork{{Cidr: "1.2.5.0/24"}, {Cidr: "1002:db8::/119"}},
			valid:            true,
			description:      "IPv4-primary dual-stack (all versions)",
			openshiftVersion: "4.12.0",
		},
		{
			element:          []*models.MachineNetwork{{Cidr: "1.2.5.0/24"}, {Cidr: "1002:db8::/119"}},
			valid:            true,
			description:      "IPv4-primary dual-stack (4.13+)",
			openshiftVersion: "4.13.0",
		},
		{
			element:          []*models.MachineNetwork{{Cidr: "1002:db8::/119"}, {Cidr: "1.2.5.0/24"}},
			valid:            false,
			description:      "IPv6-primary dual-stack (not supported in 4.11)",
			openshiftVersion: "4.11.0",
		},
		{
			element:          []*models.MachineNetwork{{Cidr: "1002:db8::/119"}, {Cidr: "1.2.5.0/24"}},
			valid:            true,
			description:      "IPv6-primary dual-stack (supported in 4.13+)",
			openshiftVersion: "4.13.0",
		},
		{
			element:          []*models.MachineNetwork{{Cidr: "1002:db8::/119"}, {Cidr: "1.2.5.0/24"}},
			valid:            true,
			description:      "IPv6-primary dual-stack (supported in 4.14+)",
			openshiftVersion: "4.14.0",
		},
		{
			// Invalid because violates the "exactly 2 networks" constraint
			element:          []*models.MachineNetwork{{Cidr: "1.2.5.0/24"}, {Cidr: "1002:db8::/119"}, {Cidr: "1.2.6.0/24"}, {Cidr: "1.2.7.0/24"}},
			valid:            false,
			description:      "too many networks",
			openshiftVersion: "4.13.0",
		},
		{
			// Invalid because missing IPv4 network
			element:          []*models.MachineNetwork{{Cidr: "1002:db8::/119"}, {Cidr: "1003:db8::/119"}},
			valid:            false,
			description:      "dual IPv6 without IPv4",
			openshiftVersion: "4.13.0",
		},
		{
			// Invalid because missing IPv6 network
			element:          []*models.MachineNetwork{{Cidr: "1.2.5.0/24"}, {Cidr: "1.2.6.0/24"}},
			valid:            false,
			description:      "dual IPv4 without IPv6",
			openshiftVersion: "4.13.0",
		},
	}
	for _, test := range tests {
		t := test
		It(fmt.Sprintf("Dual-stack machine network validation: %s (OCP %s). Networks: %v", t.description, t.openshiftVersion, t.element), func() {
			if t.valid {
				Expect(network.VerifyMachineNetworksDualStack(t.element, true, t.openshiftVersion)).ToNot(HaveOccurred())
			} else {
				Expect(network.VerifyMachineNetworksDualStack(t.element, true, t.openshiftVersion)).To(HaveOccurred())
			}
		})
	}
})

var _ = Describe("Parse functions", func() {
	var log = testutil.Log()
	Context("ParseMirrorRegistries", func() {
		It("doesn't add anything to public registries if mirror registries is empty", func() {
			registries := map[string]bool{"test.com": false, "example.com": true}
			ParseMirrorRegistries(log, registries, nil)
			Expect(registries).To(Equal(map[string]bool{"test.com": false, "example.com": true}))
		})
		It("adds to registries if there are mirror registries", func() {
			registries := map[string]bool{"test.com": false, "example.com": true}
			ParseMirrorRegistries(log, registries, []mirrorregistries.RegistriesConf{{Location: "redhat.io"}})
			Expect(registries).To(Equal(map[string]bool{"test.com": false, "example.com": true, "redhat.io": true}))
		})
		It("modifies registries if mirror registries contains one from the registries list", func() {
			registries := map[string]bool{"test.com": false, "example.com": true}
			ParseMirrorRegistries(log, registries, []mirrorregistries.RegistriesConf{{Location: "test.com"}})
			Expect(registries).To(Equal(map[string]bool{"test.com": true, "example.com": true}))
		})
	})
})

var _ = Describe("ValidateClusterUpdateVIPAddresses - partial network updates", func() {
	var cluster common.Cluster

	BeforeEach(func() {
		cluster = common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.14.0",
				CPUArchitecture:  "x86_64",
				MachineNetworks: []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
					{Cidr: "2001:db8::/64"},
				},
				ServiceNetworks: []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "fd02::/112"},
				},
				ClusterNetworks: []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "fd01::/48", HostPrefix: 64},
				},
				APIVips: []*models.APIVip{
					{IP: "192.168.1.100"},
					{IP: "2001:db8::100"},
				},
				IngressVips: []*models.IngressVip{
					{IP: "192.168.1.101"},
					{IP: "2001:db8::101"},
				},
			},
		}
	})

	It("preserves MachineNetworks when only updating ClusterNetworks in dual-stack cluster", func() {
		params := &models.V2ClusterUpdateParams{
			ClusterNetworks: []*models.ClusterNetwork{
				{Cidr: "10.128.0.0/15", HostPrefix: 22},
				{Cidr: "fd01::/48", HostPrefix: 66},
			},
		}

		err := ValidateClusterUpdateVIPAddresses(true, &cluster, params, nil)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("allows updating only APIVips in dual-stack cluster", func() {
		params := &models.V2ClusterUpdateParams{
			APIVips: []*models.APIVip{
				{IP: "192.168.1.102"},
				{IP: "2001:db8::102"},
			},
		}

		err := ValidateClusterUpdateVIPAddresses(true, &cluster, params, nil)
		Expect(err).ShouldNot(HaveOccurred())
	})
})

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster validations tests")
}
