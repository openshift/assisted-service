package slowquery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

const (
	ScopeClusterMonitor = "cluster-monitor"
	ScopeHostMonitor    = "host-monitor"
	ScopeHTTP           = "http"
)

type ctxKey int

const (
	ctxScope ctxKey = iota
	ctxRoute
)

// Config controls which parts of the application emit GORM slow-query logs.
type Config struct {
	// EnabledScopes lists scopes that may emit slow-query logs (e.g. cluster-monitor, host-monitor, http).
	EnabledScopes map[string]struct{}
	// DefaultThreshold is used when a scope does not define its own threshold.
	DefaultThreshold time.Duration
	// HTTPRoutes limits HTTP scope logging to matching route labels. Empty means all HTTP routes.
	HTTPRoutes []string
}

// AllScopes returns the built-in scope names.
func AllScopes() []string {
	return []string{ScopeClusterMonitor, ScopeHostMonitor, ScopeHTTP}
}

// ParseScopes parses a comma-separated scope list.
func ParseScopes(raw string) map[string]struct{} {
	scopes := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		scope := strings.TrimSpace(part)
		if scope != "" {
			scopes[scope] = struct{}{}
		}
	}
	return scopes
}

// ParseHTTPRoutes parses a comma-separated HTTP route filter list.
func ParseHTTPRoutes(raw string) []string {
	var routes []string
	for _, part := range strings.Split(raw, ",") {
		route := strings.TrimSpace(part)
		if route != "" {
			routes = append(routes, route)
		}
	}
	return routes
}

// NewConfig builds a Config. When scopes is empty and legacyGlobal is true, all built-in scopes are enabled.
func NewConfig(scopes string, legacyGlobal bool, defaultThreshold time.Duration, httpRoutes string) Config {
	enabled := ParseScopes(scopes)
	if len(enabled) == 0 && legacyGlobal {
		enabled = ParseScopes(strings.Join(AllScopes(), ","))
	}
	return Config{
		EnabledScopes:    enabled,
		DefaultThreshold: defaultThreshold,
		HTTPRoutes:       ParseHTTPRoutes(httpRoutes),
	}
}

func (c Config) Enabled() bool {
	return len(c.EnabledScopes) > 0
}

func (c Config) scopeEnabled(scope string) bool {
	_, ok := c.EnabledScopes[scope]
	return ok
}

func (c Config) matchesHTTPRoute(route string) bool {
	if len(c.HTTPRoutes) == 0 {
		return true
	}
	route = strings.ToLower(route)
	for _, pattern := range c.HTTPRoutes {
		if strings.Contains(route, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func (c Config) shouldLog(scope, route string) bool {
	if scope == "" || !c.scopeEnabled(scope) {
		return false
	}
	if scope == ScopeHTTP && !c.matchesHTTPRoute(route) {
		return false
	}
	return true
}

func (c Config) threshold(_ string) time.Duration {
	if c.DefaultThreshold > 0 {
		return c.DefaultThreshold
	}
	return 200 * time.Millisecond
}

// WithScope annotates ctx with a slow-query logging scope for context-aware DB calls.
func WithScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, ctxScope, scope)
}

// WithRoute annotates ctx with an HTTP route label (operation ID or method+path).
func WithRoute(ctx context.Context, route string) context.Context {
	return context.WithValue(ctx, ctxRoute, route)
}

func scopeFromContext(ctx context.Context) (scope, route string) {
	if ctx == nil {
		return "", ""
	}
	if v, ok := ctx.Value(ctxScope).(string); ok {
		scope = v
	}
	if v, ok := ctx.Value(ctxRoute).(string); ok {
		route = v
	}
	return scope, route
}

type goroutineScope struct {
	scope string
	route string
}

var goroutineScopes sync.Map

// SetGoroutineScope marks the current goroutine with a slow-query scope until ClearGoroutineScope is called.
// This is used for HTTP handlers and background jobs that do not pass context into every DB call.
func SetGoroutineScope(scope, route string) {
	goroutineScopes.Store(currentGoID(), goroutineScope{scope: scope, route: route})
}

// ClearGoroutineScope removes the scope for the current goroutine.
func ClearGoroutineScope() {
	goroutineScopes.Delete(currentGoID())
}

func goroutineScopeFromRegistry() (scope, route string, ok bool) {
	v, found := goroutineScopes.Load(currentGoID())
	if !found {
		return "", "", false
	}
	gs := v.(goroutineScope)
	return gs.scope, gs.route, true
}

func resolveScope(ctx context.Context) (scope, route string) {
	scope, route = scopeFromContext(ctx)
	if scope != "" {
		return scope, route
	}
	scope, route, ok := goroutineScopeFromRegistry()
	if ok {
		return scope, route
	}
	return "", ""
}

func currentGoID() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	return idField
}

// NewLogger returns a GORM logger that emits slow SQL only for enabled scopes.
func NewLogger(cfg Config, out io.Writer) logger.Interface {
	if out == nil {
		out = os.Stdout
	}
	return &scopedLogger{
		cfg:    cfg,
		writer: log.New(out, "\r\n", log.LstdFlags),
		traceStr:     "%s\n[%.3fms] [rows:%v] [scope:%s] %s",
		traceWarnStr: "%s %s\n[%.3fms] [rows:%v] [scope:%s] %s",
		traceErrStr:  "%s %s\n[%.3fms] [rows:%v] [scope:%s] %s",
	}
}

type scopedLogger struct {
	cfg Config
	writer logger.Writer
	traceStr, traceWarnStr, traceErrStr string
}

func (l *scopedLogger) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

func (l *scopedLogger) Info(context.Context, string, ...interface{}) {}

func (l *scopedLogger) Warn(context.Context, string, ...interface{}) {}

func (l *scopedLogger) Error(context.Context, string, ...interface{}) {}

func (l *scopedLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if !l.cfg.Enabled() {
		return
	}

	scope, route := resolveScope(ctx)
	if !l.cfg.shouldLog(scope, route) {
		return
	}

	threshold := l.cfg.threshold(scope)
	elapsed := time.Since(begin)
	scopeLabel := scope
	if route != "" {
		scopeLabel = fmt.Sprintf("%s:%s", scope, route)
	}

	switch {
	case err != nil && (!errors.Is(err, logger.ErrRecordNotFound)):
		sql, rows := fc()
		if rows == -1 {
			l.writer.Printf(l.traceErrStr, utils.FileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, "-", scopeLabel, sql)
		} else {
			l.writer.Printf(l.traceErrStr, utils.FileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, rows, scopeLabel, sql)
		}
	case elapsed > threshold:
		sql, rows := fc()
		slowLog := fmt.Sprintf("SLOW SQL >= %v", threshold)
		if rows == -1 {
			l.writer.Printf(l.traceWarnStr, utils.FileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, "-", scopeLabel, sql)
		} else {
			l.writer.Printf(l.traceWarnStr, utils.FileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, rows, scopeLabel, sql)
		}
	}
}
