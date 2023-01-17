package stream

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/versions"
	kafka "github.com/segmentio/kafka-go"
)

var _ = Describe("Produce message", func() {
	var (
		ctx          = context.Background()
		writer       *KafkaWriter
		ctrl         *gomock.Controller
		mockProducer *MockKafkaProducer
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockProducer = NewMockKafkaProducer(ctrl)
		writer = &KafkaWriter{
			kafkaProducer: mockProducer,
		}
	})

	When("Write message without metadata", func() {
		It("writes message to kafka", func() {
			key := []byte("my-key")
			payload := map[string]interface{}{
				"foo": "bar",
				"bar": "qux",
			}

			envelope := &EventEnvelope{
				Name:     "TestEvent",
				Payload:  payload,
				Metadata: nil,
			}
			value, err := json.Marshal(envelope)
			Expect(err).To(BeNil())
			expectedMessage := kafka.Message{
				Key:   key,
				Value: value,
			}
			mockProducer.EXPECT().WriteMessages(ctx, expectedMessage).Times(1).Return(nil)

			err = writer.Write(ctx, "TestEvent", key, payload)

			Expect(err).To(BeNil())
		})
	})

	When("Write message with version metadata", func() {
		It("writes message to kafka", func() {

			key := []byte("my-key")
			payload := map[string]interface{}{
				"foo": "bar",
				"bar": "qux",
			}
			metadata := map[string]string{
				"foo":    "bar",
				"foobar": "barfoo",
			}
			writer.metadata = metadata

			envelope := &EventEnvelope{
				Name:     "TestEvent",
				Payload:  payload,
				Metadata: metadata,
			}
			value, err := json.Marshal(envelope)
			Expect(err).To(BeNil())
			expectedMessage := kafka.Message{
				Key:   key,
				Value: value,
			}
			mockProducer.EXPECT().WriteMessages(ctx, expectedMessage).Times(1).Return(nil)

			err = writer.Write(ctx, "TestEvent", key, payload)

			Expect(err).To(BeNil())
		})
	})

	When("Write message with version metadata", func() {
		It("writes message to kafka", func() {
			v := versions.Versions{}
			err := envconfig.Process("", &v)
			Expect(err).To(BeNil())
			versions := versions.GetModelVersions(v)

			key := []byte("my-key")
			payload := map[string]interface{}{
				"foo": "bar",
				"bar": "qux",
			}
			metadata := map[string]interface{}{
				"versions": versions,
			}
			writer.metadata = metadata

			envelope := &EventEnvelope{
				Name:     "TestEvent",
				Payload:  payload,
				Metadata: metadata,
			}
			value, err := json.Marshal(envelope)
			Expect(err).To(BeNil())
			expectedMessage := kafka.Message{
				Key:   key,
				Value: value,
			}
			mockProducer.EXPECT().WriteMessages(ctx, expectedMessage).Times(1).Return(nil)

			err = writer.Write(ctx, "TestEvent", key, payload)

			Expect(err).To(BeNil())
		})
	})

	When("Kafka producer returns error", func() {
		It("returns error", func() {
			writeError := errors.New("my-error")
			mockProducer.EXPECT().WriteMessages(ctx, gomock.Any()).Times(1).Return(writeError)

			err := writer.Write(ctx, "TestEvent", []byte(""), []byte("myvalue"))

			Expect(err).Should(Equal(writeError))
		})
	})
})

func TestProducer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Event streams suite")
}
