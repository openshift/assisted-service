package cluster

import (
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
		infraEnv := &common.InfraEnv{
			InfraEnv:   models.InfraEnv{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(infraEnv, auth.TypeRHSSO)

		Expect(err).To(HaveOccurred())
	})

	It("succeeds with rhsso auth when cloud.openshift.com pull secret is present", func() {
		infraEnv := &common.InfraEnv{
			InfraEnv:   models.InfraEnv{ID: &id},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(infraEnv, auth.TypeRHSSO)

		Expect(err).ToNot(HaveOccurred())
	})

	It("returns empty when no auth is configured", func() {
		infraEnv := &common.InfraEnv{
			InfraEnv:   models.InfraEnv{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		token, err := AgentToken(infraEnv, auth.TypeNone)
		Expect(err).ToNot(HaveOccurred())
		Expect(token).To(Equal(""))
	})

	It("returns an error if an invalid auth type is configured", func() {
		infraEnv := &common.InfraEnv{
			InfraEnv:   models.InfraEnv{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(infraEnv, auth.AuthType("asdf"))

		Expect(err).To(HaveOccurred())
	})

	It("returns an error for local auth with no private key", func() {
		infraEnv := &common.InfraEnv{
			InfraEnv:   models.InfraEnv{ID: &id},
			PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		_, err := AgentToken(infraEnv, auth.TypeLocal)

		Expect(err).To(HaveOccurred())
	})
})
