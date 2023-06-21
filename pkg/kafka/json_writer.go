package kafka

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	kafka "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/compress"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
)

const (
	saslPlain string = "PLAIN"
	saslScram string = "SCRAM"

	WriteTimeout time.Duration = 5 * time.Second
)

//go:generate mockgen -source=json_writer.go -package=kafka -destination=mock_json_writer.go

// mocking kafka-go producer for testing
type Producer interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type Config struct {
	SaslMechanism   string `envconfig:"KAFKA_SASL_MECHANISM" default:"PLAIN"`
	BootstrapServer string `envconfig:"KAFKA_BOOTSTRAP_SERVER" required:"true"`
	ClientID        string `envconfig:"KAFKA_CLIENT_ID" default:""`
	ClientSecret    string `envconfig:"KAFKA_CLIENT_SECRET" default:""`
	Topic           string `envconfig:"KAFKA_EVENT_STREAM_TOPIC" required:"true"`
}

type JSONWriter struct {
	producer Producer
}

func getMechanism(config *Config) (sasl.Mechanism, error) {
	if config.ClientID == "" || config.ClientSecret == "" {
		// no credentials set, possibly using unauthenticated connection
		return nil, nil
	}
	if config.SaslMechanism == saslPlain {
		return &plain.Mechanism{
			Username: config.ClientID,
			Password: config.ClientSecret,
		}, nil
	}
	if config.SaslMechanism == saslScram {
		return scram.Mechanism(scram.SHA512, config.ClientID, config.ClientSecret)

	}
	return nil, fmt.Errorf("sasl mechanism %s is not valid", config.SaslMechanism)
}

func newProducer(config *Config) (Producer, error) {
	brokers := strings.Split(config.BootstrapServer, ",")
	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        config.Topic,
		Balancer:     &kafka.ReferenceHash{},
		Compression:  compress.Gzip,
		Async:        true,
		WriteTimeout: WriteTimeout,
	}
	mechanism, err := getMechanism(config)
	if err != nil {
		return nil, err
	}
	if mechanism != nil {
		writer.Transport = &kafka.Transport{
			SASL: mechanism,
			// let config pick default root CA, but define it to force TLS
			TLS: &tls.Config{},
		}
	}
	return writer, nil
}

func NewWriter() (*JSONWriter, error) {
	config := &Config{}
	err := envconfig.Process("", config)
	if err != nil {
		return nil, err
	}

	p, err := newProducer(config)
	if err != nil {
		return nil, err
	}
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
