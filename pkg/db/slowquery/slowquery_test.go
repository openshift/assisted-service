package slowquery

import (
	"bytes"
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestSlowQuery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Slow query")
}

var _ = Describe("ParseScopes", func() {
	It("parses a comma-separated scope list", func() {
		Expect(ParseScopes(" cluster-monitor , host-monitor,http ")).To(Equal(map[string]struct{}{
			ScopeClusterMonitor: {},
			ScopeHostMonitor:    {},
			ScopeHTTP:           {},
		}))
	})
})

var _ = Describe("NewConfig", func() {
	It("enables all built-in scopes when legacy global logging is on and scopes are empty", func() {
		cfg := NewConfig("", true, 100*time.Millisecond, "")
		Expect(cfg.Enabled()).To(BeTrue())
		Expect(cfg.scopeEnabled(ScopeClusterMonitor)).To(BeTrue())
		Expect(cfg.scopeEnabled(ScopeHTTP)).To(BeTrue())
	})
})

var _ = Describe("Config HTTP route filter", func() {
	It("matches only configured HTTP routes", func() {
		cfg := NewConfig(ScopeHTTP, false, time.Millisecond, "V2UpdateCluster")
		Expect(cfg.shouldLog(ScopeHTTP, "V2UpdateCluster")).To(BeTrue())
		Expect(cfg.shouldLog(ScopeHTTP, "V2GetClusters")).To(BeFalse())
	})

	It("accepts operation ID route labels", func() {
		cfg := NewConfig(ScopeHTTP, false, time.Millisecond, "V2GetCluster")
		Expect(cfg.shouldLog(ScopeHTTP, "V2GetCluster")).To(BeTrue())
	})
})

var _ = Describe("scopedLogger", func() {
	It("logs slow SQL only for enabled scopes", func() {
		var buf bytes.Buffer
		cfg := NewConfig(ScopeClusterMonitor, false, time.Millisecond, "")
		log := NewLogger(cfg, &buf)

		ctx := WithScope(context.Background(), ScopeClusterMonitor)
		log.Trace(ctx, time.Now().Add(-2*time.Millisecond), func() (string, int64) { return "SELECT 1", 1 }, nil)
		Expect(buf.String()).To(ContainSubstring("SLOW SQL"))
		Expect(buf.String()).To(ContainSubstring(ScopeClusterMonitor))

		buf.Reset()
		log.Trace(context.Background(), time.Now().Add(-2*time.Millisecond), func() (string, int64) { return "SELECT 2", 1 }, nil)
		Expect(buf.String()).To(BeEmpty())
	})

	It("resolves scope from the goroutine registry", func() {
		var buf bytes.Buffer
		cfg := NewConfig(ScopeHostMonitor, false, time.Millisecond, "")
		log := NewLogger(cfg, &buf)

		SetGoroutineScope(ScopeHostMonitor, "")
		defer ClearGoroutineScope()

		log.Trace(context.Background(), time.Now().Add(-2*time.Millisecond), func() (string, int64) { return "SELECT 3", 1 }, nil)
		Expect(buf.String()).To(ContainSubstring(ScopeHostMonitor))
	})

	It("does not interpolate bound parameter values into logged SQL", func() {
		var buf bytes.Buffer
		l := NewLogger(NewConfig(ScopeClusterMonitor, false, time.Millisecond, ""), &buf).(*scopedLogger)
		ctx := WithScope(context.Background(), ScopeClusterMonitor)

		sql, params := l.ParamsFilter(ctx, "SELECT * FROM clusters WHERE pull_secret = $1", "top-secret-value")
		Expect(sql).To(Equal("SELECT * FROM clusters WHERE pull_secret = $1"))
		Expect(params).To(BeNil())

		l.Trace(ctx, time.Now().Add(-2*time.Millisecond), func() (string, int64) {
			return sql, 1
		}, nil)
		Expect(buf.String()).To(ContainSubstring("$1"))
		Expect(buf.String()).NotTo(ContainSubstring("top-secret-value"))
	})

	It("redacts string literals from logged SQL", func() {
		Expect(redactSQLLiterals("SELECT * FROM hosts WHERE email = 'user@example.com'")).To(
			Equal("SELECT * FROM hosts WHERE email = '?'"),
		)
		Expect(redactSQLLiterals("SELECT * FROM clusters WHERE pull_secret = $1")).To(
			Equal("SELECT * FROM clusters WHERE pull_secret = $1"),
		)
		Expect(redactSQLLiterals("WHERE name = 'O''Reilly'")).To(Equal("WHERE name = '?'"))

		var buf bytes.Buffer
		l := NewLogger(NewConfig(ScopeClusterMonitor, false, time.Millisecond, ""), &buf).(*scopedLogger)
		ctx := WithScope(context.Background(), ScopeClusterMonitor)
		l.Trace(ctx, time.Now().Add(-2*time.Millisecond), func() (string, int64) {
			return "SELECT * FROM clusters WHERE name = 'production-cluster'", 1
		}, nil)
		Expect(buf.String()).To(ContainSubstring("name = '?'"))
		Expect(buf.String()).NotTo(ContainSubstring("production-cluster"))
	})
})
