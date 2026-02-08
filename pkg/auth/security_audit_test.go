package auth

import (
	"bytes"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("SecurityAuditLogger", func() {
	var (
		logger    *SecurityAuditLogger
		logBuffer *bytes.Buffer
		log       *logrus.Logger
	)

	BeforeEach(func() {
		logBuffer = &bytes.Buffer{}
		log = logrus.New()
		log.SetOutput(logBuffer)
		log.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
		logger = NewSecurityAuditLogger(log)
	})

	Describe("LogSuccessfulLogin", func() {
		It("logs successful login with username and IP", func() {
			logger.LogSuccessfulLogin("testuser", "192.168.1.1")

			output := logBuffer.String()
			Expect(output).To(ContainSubstring("Successful login"))
			Expect(output).To(ContainSubstring("testuser"))
			Expect(output).To(ContainSubstring("192.168.1.1"))
			Expect(output).To(ContainSubstring(string(SecurityEventLoginSuccess)))
		})
	})

	Describe("LogFailedLogin", func() {
		It("logs failed login with details", func() {
			logger.LogFailedLogin("testuser", "192.168.1.1", 3, "invalid password")

			output := logBuffer.String()
			Expect(output).To(ContainSubstring("Failed login attempt"))
			Expect(output).To(ContainSubstring("testuser"))
			Expect(output).To(ContainSubstring("192.168.1.1"))
			Expect(output).To(ContainSubstring("invalid password"))
			Expect(output).To(ContainSubstring(string(SecurityEventLoginFailure)))
		})
	})

	Describe("LogAccountLocked", func() {
		It("logs account lockout with details", func() {
			lockedUntil := time.Now().Add(15 * time.Minute)
			logger.LogAccountLocked("testuser", "192.168.1.1", 5, lockedUntil)

			output := logBuffer.String()
			Expect(output).To(ContainSubstring("Account locked"))
			Expect(output).To(ContainSubstring("testuser"))
			Expect(output).To(ContainSubstring(string(SecurityEventAccountLocked)))
		})
	})

	Describe("LogLockedLoginAttempt", func() {
		It("logs attempt on locked account", func() {
			lockedUntil := time.Now().Add(15 * time.Minute)
			logger.LogLockedLoginAttempt("testuser", "192.168.1.1", lockedUntil)

			output := logBuffer.String()
			Expect(output).To(ContainSubstring("Login attempt on locked account"))
			Expect(output).To(ContainSubstring("testuser"))
			Expect(output).To(ContainSubstring(string(SecurityEventLockedLoginAttempt)))
		})
	})

	Describe("LogIPLocked", func() {
		It("logs IP lockout", func() {
			lockedUntil := time.Now().Add(15 * time.Minute)
			logger.LogIPLocked("192.168.1.1", 10, lockedUntil)

			output := logBuffer.String()
			Expect(output).To(ContainSubstring("IP locked"))
			Expect(output).To(ContainSubstring("192.168.1.1"))
			Expect(output).To(ContainSubstring(string(SecurityEventIPLocked)))
		})
	})

	Describe("LogLockedIPAttempt", func() {
		It("logs attempt from locked IP", func() {
			lockedUntil := time.Now().Add(15 * time.Minute)
			logger.LogLockedIPAttempt("192.168.1.1", lockedUntil)

			output := logBuffer.String()
			Expect(output).To(ContainSubstring("Login attempt from locked IP"))
			Expect(output).To(ContainSubstring("192.168.1.1"))
			Expect(output).To(ContainSubstring(string(SecurityEventLockedIPAttempt)))
		})
	})

	Describe("Component field", func() {
		It("includes security_audit component in logs", func() {
			logger.LogSuccessfulLogin("testuser", "192.168.1.1")

			output := logBuffer.String()
			Expect(strings.Contains(output, "security_audit")).To(BeTrue())
		})
	})
})
