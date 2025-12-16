package testing

import (
	"github.com/openshift/assisted-service/internal/stream"
	"go.uber.org/mock/gomock"
)

func GetDummyNotificationStream(ctrl *gomock.Controller) *stream.MockNotifier {
	dummyStream := stream.NewMockNotifier(ctrl)
	dummyStream.EXPECT().Notify(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	return dummyStream
}
