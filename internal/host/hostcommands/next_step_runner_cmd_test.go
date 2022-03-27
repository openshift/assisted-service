package hostcommands

import (
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

func getNextStepRequest(args []string) *models.NextStepCmdRequest {
	request := models.NextStepCmdRequest{}
	err := json.Unmarshal([]byte(args[0]), &request)
	Expect(err).NotTo(HaveOccurred())
	return &request
}

var _ = Describe("Format command for starting next step agent", func() {

	var config NextStepRunnerConfig

	infraEnvId := strfmt.UUID(uuid.New().String())
	hostID := strfmt.UUID(uuid.New().String())
	image := uuid.New().String()

	BeforeEach(func() {
		config = NextStepRunnerConfig{
			InfraEnvID:          infraEnvId,
			HostID:              hostID,
			NextStepRunnerImage: image,
		}
	})

	It("standard formatting", func() {
		command, args, err := GetNextStepRunnerCommand(&config)
		Expect(err).ToNot(HaveOccurred())
		Expect(command).Should(Equal(""))
		request := getNextStepRequest(*args)
		Expect(request.HostID.String()).Should(Equal(hostID.String()))
		Expect(request.InfraEnvID.String()).Should(Equal(infraEnvId.String()))
		Expect(swag.StringValue(request.AgentVersion)).Should(Equal(image))
	})

})
