package slowquery

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseScopes(t *testing.T) {
	scopes := ParseScopes(" cluster-monitor , host-monitor,http ")
	assert.Equal(t, map[string]struct{}{
		ScopeClusterMonitor: {},
		ScopeHostMonitor:    {},
		ScopeHTTP:           {},
	}, scopes)
}

func TestNewConfigLegacyGlobal(t *testing.T) {
	cfg := NewConfig("", true, 100*time.Millisecond, "")
	assert.True(t, cfg.Enabled())
	assert.True(t, cfg.scopeEnabled(ScopeClusterMonitor))
	assert.True(t, cfg.scopeEnabled(ScopeHTTP))
}

func TestHTTPRouteFilter(t *testing.T) {
	cfg := NewConfig(ScopeHTTP, false, time.Millisecond, "V2UpdateCluster")
	assert.True(t, cfg.shouldLog(ScopeHTTP, "V2UpdateCluster"))
	assert.False(t, cfg.shouldLog(ScopeHTTP, "V2GetClusters"))
}

func TestScopedLoggerOnlyLogsEnabledScope(t *testing.T) {
	var buf bytes.Buffer
	cfg := NewConfig(ScopeClusterMonitor, false, time.Millisecond, "")
	log := NewLogger(cfg, &buf)

	ctx := WithScope(context.Background(), ScopeClusterMonitor)
	log.Trace(ctx, time.Now().Add(-2*time.Millisecond), func() (string, int64) { return "SELECT 1", 1 }, nil)
	assert.Contains(t, buf.String(), "SLOW SQL")
	assert.Contains(t, buf.String(), ScopeClusterMonitor)

	buf.Reset()
	log.Trace(context.Background(), time.Now().Add(-2*time.Millisecond), func() (string, int64) { return "SELECT 2", 1 }, nil)
	assert.Empty(t, buf.String())
}

func TestGoroutineScope(t *testing.T) {
	var buf bytes.Buffer
	cfg := NewConfig(ScopeHostMonitor, false, time.Millisecond, "")
	log := NewLogger(cfg, &buf)

	SetGoroutineScope(ScopeHostMonitor, "")
	defer ClearGoroutineScope()

	log.Trace(context.Background(), time.Now().Add(-2*time.Millisecond), func() (string, int64) { return "SELECT 3", 1 }, nil)
	assert.True(t, strings.Contains(buf.String(), ScopeHostMonitor))
}

func TestRouteLabelUsesOperationID(t *testing.T) {
	cfg := NewConfig(ScopeHTTP, false, time.Millisecond, "V2GetCluster")
	assert.True(t, cfg.shouldLog(ScopeHTTP, "V2GetCluster"))
}
