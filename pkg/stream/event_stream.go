package stream

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"time"

	"github.com/kelseyhightower/envconfig"
	kafka "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/compress"
	"github.com/segmentio/kafka-go/sasl/plain"
)

const (
	WriteTimeout time.Duration = 5 * time.Second
)

//go:generate mockgen -source=event_stream.go -package=stream -destination=mock_event_stream.go

type KafkaProducer interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type EventEnvelope struct {
	Name     string      `json:"name"`
	Payload  interface{} `json:"payload"`
	Metadata interface{} `json:"metadata"`
}

type EventStreamWriter interface {
	Write(ctx context.Context, eventName string, key []byte, payload interface{}) error
	Close()
}

type KafkaConfig struct {
	BootstrapServer string `envconfig:"KAFKA_BOOTSTRAP_SERVER" required:"true"`
	ClientID        string `envconfig:"KAFKA_CLIENT_ID" default:""`
	ClientSecret    string `envconfig:"KAFKA_CLIENT_SECRET" default:""`
	Topic           string `envconfig:"KAFKA_EVENT_STREAM_TOPIC" required:"true"`
}

type KafkaWriter struct {
	metadata      interface{}
	kafkaProducer KafkaProducer
}

func newProducer(config *KafkaConfig) KafkaProducer {

	writer := &kafka.Writer{
		Addr:         kafka.TCP(config.BootstrapServer),
		Topic:        config.Topic,
		Balancer:     &kafka.ReferenceHash{},
		Compression:  compress.Gzip,
		Async:        true,
		WriteTimeout: WriteTimeout,
	}
	if config.ClientID != "" && config.ClientSecret != "" {
		mechanism := &plain.Mechanism{
			Username: config.ClientID,
			Password: config.ClientSecret,
		}
		writer.Transport = &kafka.Transport{
			SASL: mechanism,
			// let config pick default root CA, but define it to force TLS
			TLS: &tls.Config{},
		}
	}
	return writer
}

func NewKafkaWriterWithMetadata(metadata interface{}) (*KafkaWriter, error) {
	writer, err := NewKafkaWriter()
	writer.metadata = metadata
	return writer, err
}

func NewKafkaWriter() (*KafkaWriter, error) {
	config := &KafkaConfig{}
	err := envconfig.Process("", config)
	if err != nil {
		return nil, err
	}

	p := newProducer(config)
	return &KafkaWriter{
		kafkaProducer: p,
	}, nil
}

func (w *KafkaWriter) Close() {
	w.kafkaProducer.Close()
}

func (w *KafkaWriter) Write(ctx context.Context, eventName string, key []byte, payload interface{}) error {
	envelope := &EventEnvelope{
		Name:     eventName,
		Payload:  payload,
		Metadata: w.metadata,
	}
	value, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	msg := kafka.Message{
		Key:   key,
		Value: value,
	}
	// If Async is true, this will always return nil
	return w.kafkaProducer.WriteMessages(ctx, msg)
}
