package auth

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LockoutPolicy", func() {
	var policy LockoutPolicy

	BeforeEach(func() {
		policy = DefaultLockoutPolicy()
	})

	Describe("DefaultLockoutPolicy", func() {
		It("returns sensible defaults", func() {
			p := DefaultLockoutPolicy()
			Expect(p.MaxAttempts).To(Equal(5))
			Expect(p.LockoutDuration).To(Equal(15 * time.Minute))
			Expect(p.WindowDuration).To(Equal(5 * time.Minute))
			Expect(p.UseExponential).To(BeTrue())
			Expect(p.Enabled).To(BeTrue())
		})
	})

	Describe("CalculateLockout", func() {
		Context("with exponential backoff disabled", func() {
			BeforeEach(func() {
				policy.UseExponential = false
			})

			It("returns 0 for attempts below threshold", func() {
				for i := 0; i < policy.MaxAttempts; i++ {
					Expect(policy.CalculateLockout(i)).To(Equal(time.Duration(0)))
				}
			})

			It("returns fixed lockout duration at threshold", func() {
				Expect(policy.CalculateLockout(policy.MaxAttempts)).To(Equal(policy.LockoutDuration))
			})

			It("returns fixed lockout duration above threshold", func() {
				Expect(policy.CalculateLockout(policy.MaxAttempts + 5)).To(Equal(policy.LockoutDuration))
			})
		})

		Context("with exponential backoff enabled", func() {
			BeforeEach(func() {
				policy.UseExponential = true
				policy.LockoutDuration = 1 * time.Minute // Use 1 minute for easier calculation
			})

			It("returns 0 for attempts below threshold", func() {
				for i := 0; i < policy.MaxAttempts; i++ {
					Expect(policy.CalculateLockout(i)).To(Equal(time.Duration(0)))
				}
			})

			It("returns base lockout duration at threshold", func() {
				Expect(policy.CalculateLockout(policy.MaxAttempts)).To(Equal(policy.LockoutDuration))
			})

			It("doubles lockout duration with each additional attempt", func() {
				// At threshold: 1 minute
				Expect(policy.CalculateLockout(policy.MaxAttempts)).To(Equal(1 * time.Minute))
				// threshold + 1: 2 minutes
				Expect(policy.CalculateLockout(policy.MaxAttempts + 1)).To(Equal(2 * time.Minute))
				// threshold + 2: 4 minutes
				Expect(policy.CalculateLockout(policy.MaxAttempts + 2)).To(Equal(4 * time.Minute))
				// threshold + 3: 8 minutes
				Expect(policy.CalculateLockout(policy.MaxAttempts + 3)).To(Equal(8 * time.Minute))
			})

			It("caps exponential growth at 10 doublings", func() {
				// At threshold + 10: 1024 minutes (capped)
				capped := policy.CalculateLockout(policy.MaxAttempts + 10)
				// At threshold + 15: should still be 1024 minutes
				stillCapped := policy.CalculateLockout(policy.MaxAttempts + 15)
				Expect(stillCapped).To(Equal(capped))
			})
		})
	})

	Describe("ShouldLock", func() {
		It("returns false below threshold", func() {
			for i := 0; i < policy.MaxAttempts; i++ {
				Expect(policy.ShouldLock(i)).To(BeFalse())
			}
		})

		It("returns true at threshold", func() {
			Expect(policy.ShouldLock(policy.MaxAttempts)).To(BeTrue())
		})

		It("returns true above threshold", func() {
			Expect(policy.ShouldLock(policy.MaxAttempts + 5)).To(BeTrue())
		})
	})

	Describe("RemainingAttempts", func() {
		It("returns correct remaining attempts", func() {
			Expect(policy.RemainingAttempts(0)).To(Equal(5))
			Expect(policy.RemainingAttempts(1)).To(Equal(4))
			Expect(policy.RemainingAttempts(4)).To(Equal(1))
			Expect(policy.RemainingAttempts(5)).To(Equal(0))
			Expect(policy.RemainingAttempts(10)).To(Equal(0))
		})
	})
})
