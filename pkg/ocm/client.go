package ocm

import (
	"context"
	"time"

	sdkClient "github.com/openshift-online/ocm-sdk-go"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Client struct {
	config     *Config
	logger     sdkClient.Logger
	connection *sdkClient.Connection
	Cache      *cache.Cache

	Authentication OCMAuthentication
	Authorization  OCMAuthorization
}

type Config struct {
	BaseURL      string `envconfig:"OCM_BASE_URL" default:""`
	ClientID     string `envconfig:"OCM_SERVICE_CLIENT_ID" default:""`
	ClientSecret string `envconfig:"OCM_SERVICE_CLIENT_SECRET" default:""`
	SelfToken    string `envconfig:"OCM_SELF_TOKEN" default:""`
	TokenURL     string `envconfig:"OCM_TOKEN_URL" default:""`
	LogLevel     string `envconfig:"OCM_LOG_LEVEL" default:"info"`
}

type SdKLogger struct {
	Log         *logrus.Logger
	FieldLogger logrus.FieldLogger
}

func (l *SdKLogger) DebugEnabled() bool {
	return l.Log.IsLevelEnabled(logrus.DebugLevel)
}

func (l *SdKLogger) InfoEnabled() bool {
	return l.Log.IsLevelEnabled(logrus.InfoLevel)
}

func (l *SdKLogger) WarnEnabled() bool {
	return l.Log.IsLevelEnabled(logrus.WarnLevel)
}

func (l *SdKLogger) ErrorEnabled() bool {
	return l.Log.IsLevelEnabled(logrus.ErrorLevel)
}

func (l *SdKLogger) Debug(ctx context.Context, format string, args ...interface{}) {
	l.FieldLogger.Debugf(format, args...)
}

func (l *SdKLogger) Info(ctx context.Context, format string, args ...interface{}) {
	l.FieldLogger.Infof(format, args...)
}

func (l *SdKLogger) Warn(ctx context.Context, format string, args ...interface{}) {
	l.FieldLogger.Warnf(format, args...)
}

func (l *SdKLogger) Error(ctx context.Context, format string, args ...interface{}) {
	l.FieldLogger.Errorf(format, args...)
}

func NewClient(config Config, log logrus.FieldLogger) (*Client, error) {
	entry := log.(*logrus.Entry)
	logger := &SdKLogger{Log: entry.Logger, FieldLogger: log}
	if logLevel, err := logrus.ParseLevel(config.LogLevel); err == nil {
		logger.Log.SetLevel(logLevel)
	}

	client := &Client{
		config: &config,
		logger: logger,
		Cache:  cache.New(10*time.Minute, 30*time.Minute),
	}
	err := client.newConnection()
	if err != nil {
		return nil, errors.Wrapf(err, "Unable to build OCM connection")
	}
	client.Authentication = &authentication{
		client: client,
	}
	client.Authorization = &authorization{
		client: client,
	}
	return client, nil
}

func (c *Client) newConnection() error {
	builder := sdkClient.NewConnectionBuilder().
		Logger(c.logger).
		URL(c.config.BaseURL).
		TokenURL(c.config.TokenURL).
		Metrics("api_outbound")

	if c.config.ClientID != "" && c.config.ClientSecret != "" {
		builder = builder.Client(c.config.ClientID, c.config.ClientSecret)
	} else if c.config.SelfToken != "" {
		builder = builder.Tokens(c.config.SelfToken)
	} else {
		return errors.Errorf("Can't build OCM client connection. No Client/Secret or Token has been provided.")
	}

	connection, err := builder.Build()

	if err != nil {
		return errors.Wrapf(err, "Can't build OCM client connection")
	}
	c.connection = connection
	return nil
}

// AuthPayload defines the structure of the User
type AuthPayload struct {
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Organization string `json:"org_id"`
	Email        string `json:"email"`
	Issuer       string `json:"iss"`
	ClientID     string `json:"clientId"`
	IsAdmin      bool   `json:"is_admin"`
	IsUser       bool   `json:"is_user"`
}
