package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	kafka "github.com/segmentio/kafka-go"
)

type InvalidJSON struct {
	Value *InvalidJSON
}

var _ = Describe("Write", func() {
	var (
		ctx          = context.Background()
		writer       *JSONWriter
		ctrl         *gomock.Controller
		mockProducer *MockProducer
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockProducer = NewMockProducer(ctrl)
		writer = &JSONWriter{
			producer: mockProducer,
		}
	})

	It("should succeed when writing encodable message", func() {
		key := []byte("my-key")
		value := map[string]interface{}{
			"foo": "bar",
			"bar": "qux",
		}
		encodedValue, err := json.Marshal(value)
		Expect(err).To(BeNil())
		expectedMessage := kafka.Message{
			Key:   key,
			Value: encodedValue,
		}
		mockProducer.EXPECT().WriteMessages(ctx, expectedMessage).Times(1).Return(nil)
		err = writer.Write(ctx, key, value)
		Expect(err).To(BeNil())
	})

	It("should fail when writing non-encodable message", func() {
		key := []byte("my-key")

		invalidJSON := InvalidJSON{}
		invalidJSON.Value = &invalidJSON

		mockProducer.EXPECT().WriteMessages(ctx, gomock.Any()).Times(0)
		err := writer.Write(ctx, key, invalidJSON)
		Expect(err).NotTo(BeNil())
	})

	When("Kafka producer returns error", func() {
		It("returns error", func() {
			writeError := errors.New("my-error")
			mockProducer.EXPECT().WriteMessages(ctx, gomock.Any()).Times(1).Return(writeError)

			err := writer.Write(ctx, []byte(""), []byte("myvalue"))
			Expect(err).Should(Equal(writeError))
		})
	})
})

func TestProducer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Event streams suite")
}
