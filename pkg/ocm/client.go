package ocm

import (
	"context"
	"strings"
	"time"

	sdkClient "github.com/openshift-online/ocm-sdk-go"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Client struct {
	Config     *Config
	logger     sdkClient.Logger
	connection *sdkClient.Connection
	Cache      *cache.Cache
	log        logrus.FieldLogger
	metricsApi metrics.API

	Authentication OCMAuthentication
	Authorization  OCMAuthorization
	AccountsMgmt   OCMAccountsMgmt
}

type Config struct {
	BaseURL      string `envconfig:"OCM_BASE_URL" default:""`
	ClientID     string `envconfig:"OCM_SERVICE_CLIENT_ID" default:""`
	ClientSecret string `envconfig:"OCM_SERVICE_CLIENT_SECRET" default:""`
	TokenURL     string `envconfig:"OCM_TOKEN_URL" default:""`

	// The OCM team is deprecating the support for offline tokens. In order to detect
	// situations where the user is trying to use that we will check if the OCM_SELF_TOKEN
	// environment variable has a value, and return an error. If the user still wants to use
	// that deprecated support then we force them to akcnowledge it by also setting the
	// ACKNOWLEDGE_DEPRECATED_OCM_SELF_TOKEN environment variable to `yes`, and we will emit
	// a warning.
	//
	// These two environment variables and the code that uses them should eventually
	// be removed.
	SelfToken                      string `envconfig:"OCM_SELF_TOKEN" default:""`
	AcknowledgeDeprecatedSelfToken string `envconfig:"ACKNOWLEDGE_DEPRECATED_OCM_SELF_TOKEN" default:""`
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

func (l *SdKLogger) Fatal(ctx context.Context, format string, args ...interface{}) {
	l.FieldLogger.Fatalf(format, args...)
}

func NewClient(config Config, log logrus.FieldLogger) (*Client, error) {
	entry := log.(*logrus.Entry)
	logger := &SdKLogger{Log: entry.Logger, FieldLogger: log}

	client := &Client{
		Config: &config,
		logger: logger,
		Cache:  cache.New(10*time.Minute, 30*time.Minute),
		log:    log,
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
	client.AccountsMgmt = &accountsMgmt{
		client: client,
	}
	return client, nil
}

func (c *Client) SetMetrics(handler metrics.API) {
	c.metricsApi = handler
}

func (c *Client) newConnection() error {
	builder := sdkClient.NewConnectionBuilder().
		Logger(c.logger).
		URL(c.Config.BaseURL).
		TokenURL(c.Config.TokenURL).
		MetricsSubsystem("api_outbound")

	if c.Config.ClientID != "" && c.Config.ClientSecret != "" {
		builder = builder.Client(c.Config.ClientID, c.Config.ClientSecret)
	} else if c.Config.SelfToken != "" {
		if strings.EqualFold(c.Config.AcknowledgeDeprecatedSelfToken, "yes") {
			c.logger.Warn(
				context.TODO(),
				"Authentication using an offline token via the 'OCM_SELF_TOKEN' "+
					"environment variable is deprecated, but you have agreed "+
					"to use it anyhow setting the environment variable "+
					"'ACKNOWLEDGE_DEPRECATED_OCM_SELF_TOKEN' to '%s'",
				c.Config.AcknowledgeDeprecatedSelfToken,
			)
			builder = builder.Tokens(c.Config.SelfToken)
		} else {
			return errors.New(
				"authentication using an offline token via the 'OCM_SELF_TOKEN' " +
					"environment variable is deprecated and disabled, but if " +
					"you really need to use it you can enable it setting the " +
					"'ACKNOWLEDGE_DEPRECATED_OCM_SELF_TOKEN' environment " +
					"variable to 'yes'",
			)
		}
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

type RoleType string

const (
	UserRole          RoleType = "user"
	ReadOnlyAdminRole RoleType = "read-only-admin"
	AdminRole         RoleType = "admin"
)

// AuthPayload defines the structure of the User
type AuthPayload struct {
	Username     string   `json:"username"`
	FirstName    string   `json:"first_name"`
	LastName     string   `json:"last_name"`
	Organization string   `json:"org_id"`
	Email        string   `json:"email"`
	Issuer       string   `json:"iss"`
	ClientID     string   `json:"clientId"`
	Role         RoleType `json:"scope"`
	IsAuthorized bool     `json:"is_authorized"`
}
