package bminventory

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/job"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
)

func GenerateDummyISOImage(l logrus.FieldLogger, generator generator.ISOInstallConfigGenerator,
	eventsHandler events.Handler) {
	var (
		dummyId   = "00000000-0000-0000-0000-000000000000"
		jobName   = fmt.Sprintf("dummyimage-%s-%s", dummyId, time.Now().Format("20060102150405"))
		imgName   = getImageName(strfmt.UUID(dummyId))
		requestID = requestid.NewID()
		log       = requestid.RequestIDLogger(l, requestID)
		cluster   common.Cluster
	)
	// create dummy job without uploading to s3, we just need to pull the image
	if err := generator.GenerateISO(requestid.ToContext(context.Background(), requestID), cluster, jobName, imgName,
		job.Dummy, eventsHandler); err != nil {
		log.WithError(err).Errorf("failed to generate dummy ISO image")
	}
}
