package auth

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ = Describe("FailedAttemptTracker", func() {
	var (
		tracker *FailedAttemptTracker
		db      *gorm.DB
		dbName  string
		policy  LockoutPolicy
		log     *logrus.Logger
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		Expect(db.AutoMigrate(&FailedLoginAttempt{})).To(Succeed())

		log = logrus.New()
		policy = LockoutPolicy{
			Enabled:         true,
			MaxAttempts:     3,
			LockoutDuration: 5 * time.Minute,
			WindowDuration:  2 * time.Minute,
			UseExponential:  false,
		}
		tracker = NewFailedAttemptTracker(db, policy, log)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Describe("RecordFailure", func() {
		It("records first failure correctly", func() {
			count, lockDuration := tracker.RecordFailure("testuser", IdentifierTypeUsername)
			Expect(count).To(Equal(1))
			Expect(lockDuration).To(Equal(time.Duration(0)))
		})

		It("increments failure count", func() {
			tracker.RecordFailure("testuser", IdentifierTypeUsername)
			count, _ := tracker.RecordFailure("testuser", IdentifierTypeUsername)
			Expect(count).To(Equal(2))
		})

		It("triggers lockout at threshold", func() {
			for i := 0; i < policy.MaxAttempts-1; i++ {
				tracker.RecordFailure("testuser", IdentifierTypeUsername)
			}
			count, lockDuration := tracker.RecordFailure("testuser", IdentifierTypeUsername)
			Expect(count).To(Equal(policy.MaxAttempts))
			Expect(lockDuration).To(Equal(policy.LockoutDuration))
		})

		It("tracks different identifiers separately", func() {
			tracker.RecordFailure("user1", IdentifierTypeUsername)
			tracker.RecordFailure("user1", IdentifierTypeUsername)

			count, _ := tracker.RecordFailure("user2", IdentifierTypeUsername)
			Expect(count).To(Equal(1))
		})

		It("tracks different identifier types separately", func() {
			tracker.RecordFailure("testuser", IdentifierTypeUsername)
			tracker.RecordFailure("testuser", IdentifierTypeUsername)

			count, _ := tracker.RecordFailure("testuser", IdentifierTypeIP)
			Expect(count).To(Equal(1))
		})
	})

	Describe("IsLocked", func() {
		It("returns false when not locked", func() {
			locked, _ := tracker.IsLocked("testuser", IdentifierTypeUsername)
			Expect(locked).To(BeFalse())
		})

		It("returns true when locked", func() {
			// Trigger lockout
			for i := 0; i < policy.MaxAttempts; i++ {
				tracker.RecordFailure("testuser", IdentifierTypeUsername)
			}

			locked, until := tracker.IsLocked("testuser", IdentifierTypeUsername)
			Expect(locked).To(BeTrue())
			Expect(until).To(BeTemporally(">", time.Now()))
			Expect(until).To(BeTemporally("<=", time.Now().Add(policy.LockoutDuration+time.Second)))
		})

		It("returns false for unknown identifier", func() {
			locked, _ := tracker.IsLocked("unknown", IdentifierTypeUsername)
			Expect(locked).To(BeFalse())
		})
	})

	Describe("Reset", func() {
		It("clears failed attempts", func() {
			tracker.RecordFailure("testuser", IdentifierTypeUsername)
			tracker.RecordFailure("testuser", IdentifierTypeUsername)

			tracker.Reset("testuser", IdentifierTypeUsername)

			count := tracker.GetAttemptCount("testuser", IdentifierTypeUsername)
			Expect(count).To(Equal(0))
		})

		It("clears lockout", func() {
			// Trigger lockout
			for i := 0; i < policy.MaxAttempts; i++ {
				tracker.RecordFailure("testuser", IdentifierTypeUsername)
			}

			tracker.Reset("testuser", IdentifierTypeUsername)

			locked, _ := tracker.IsLocked("testuser", IdentifierTypeUsername)
			Expect(locked).To(BeFalse())
		})
	})

	Describe("GetAttemptCount", func() {
		It("returns 0 for unknown identifier", func() {
			count := tracker.GetAttemptCount("unknown", IdentifierTypeUsername)
			Expect(count).To(Equal(0))
		})

		It("returns correct count", func() {
			tracker.RecordFailure("testuser", IdentifierTypeUsername)
			tracker.RecordFailure("testuser", IdentifierTypeUsername)

			count := tracker.GetAttemptCount("testuser", IdentifierTypeUsername)
			Expect(count).To(Equal(2))
		})
	})

	Describe("Disabled policy", func() {
		BeforeEach(func() {
			policy.Enabled = false
			tracker = NewFailedAttemptTracker(db, policy, log)
		})

		It("does not record failures when disabled", func() {
			count, _ := tracker.RecordFailure("testuser", IdentifierTypeUsername)
			Expect(count).To(Equal(0))
		})

		It("does not report locked when disabled", func() {
			locked, _ := tracker.IsLocked("testuser", IdentifierTypeUsername)
			Expect(locked).To(BeFalse())
		})
	})

	Describe("Nil database", func() {
		BeforeEach(func() {
			tracker = NewFailedAttemptTracker(nil, policy, log)
		})

		It("handles nil database gracefully for RecordFailure", func() {
			count, _ := tracker.RecordFailure("testuser", IdentifierTypeUsername)
			Expect(count).To(Equal(0))
		})

		It("handles nil database gracefully for IsLocked", func() {
			locked, _ := tracker.IsLocked("testuser", IdentifierTypeUsername)
			Expect(locked).To(BeFalse())
		})

		It("handles nil database gracefully for Reset", func() {
			Expect(func() { tracker.Reset("testuser", IdentifierTypeUsername) }).NotTo(Panic())
		})
	})

	Describe("CleanupExpired", func() {
		It("removes expired records", func() {
			// Create a record with expired lockout
			expiredTime := time.Now().Add(-10 * time.Minute)
			attempt := FailedLoginAttempt{
				Identifier:     "expired_user",
				IdentifierType: string(IdentifierTypeUsername),
				AttemptCount:   5,
				FirstAttempt:   expiredTime,
				LastAttempt:    expiredTime,
				LockedUntil:    &expiredTime,
			}
			Expect(db.Create(&attempt).Error).To(Succeed())

			err := tracker.CleanupExpired()
			Expect(err).To(Succeed())

			// Verify record is removed
			var count int64
			db.Model(&FailedLoginAttempt{}).Where("identifier = ?", "expired_user").Count(&count)
			Expect(count).To(Equal(int64(0)))
		})

		It("keeps active records", func() {
			tracker.RecordFailure("active_user", IdentifierTypeUsername)

			err := tracker.CleanupExpired()
			Expect(err).To(Succeed())

			count := tracker.GetAttemptCount("active_user", IdentifierTypeUsername)
			Expect(count).To(Equal(1))
		})
	})
})
