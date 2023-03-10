package kafka

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

//go:generate mockgen -source=json_writer.go -package=kafka -destination=mock_json_writer.go

// mocking kafka-go producer for testing
type Producer interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type Config struct {
	BootstrapServer string `envconfig:"KAFKA_BOOTSTRAP_SERVER" required:"true"`
	ClientID        string `envconfig:"KAFKA_CLIENT_ID" default:""`
	ClientSecret    string `envconfig:"KAFKA_CLIENT_SECRET" default:""`
	Topic           string `envconfig:"KAFKA_EVENT_STREAM_TOPIC" required:"true"`
}

type JSONWriter struct {
	producer Producer
}

func newProducer(config *Config) Producer {

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

func NewWriter() (*JSONWriter, error) {
	config := &Config{}
	err := envconfig.Process("", config)
	if err != nil {
		return nil, err
	}

	p := newProducer(config)
	return &JSONWriter{
		producer: p,
	}, nil
}

func (w *JSONWriter) Close() {
	w.producer.Close()
}

func (w *JSONWriter) Write(ctx context.Context, key []byte, value interface{}) error {
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return err
	}
	msg := kafka.Message{
		Key:   key,
		Value: encodedValue,
	}
	// If Async is true, this will always return nil
	return w.producer.WriteMessages(ctx, msg)
}
