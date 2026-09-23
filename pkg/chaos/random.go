package chaos

import (
	"math"
	"math/rand/v2"
)

// RandFloatFunc defines the signature for pseudo-random float generators producing values in [0.0, 1.0).
type RandFloatFunc func() float64

// FaultInjector evaluates probabilistic chaos events without global lock contention.
type FaultInjector struct {
	defaultRate float64
	randFunc    RandFloatFunc
}

// NewFaultInjector constructs a FaultInjector with default rate and lock-free PRNG.
func NewFaultInjector(defaultRate float64) *FaultInjector {
	if defaultRate < 0.0 {
		defaultRate = 0.0
	} else if defaultRate > 1.0 {
		if defaultRate <= 100.0 {
			defaultRate /= 100.0
		} else {
			defaultRate = 1.0
		}
	}

	return &FaultInjector{
		defaultRate: defaultRate,
		randFunc:    rand.Float64,
	}
}

// SetRandFunc overrides the PRNG source (used for deterministic test assertions).
func (fi *FaultInjector) SetRandFunc(fn RandFloatFunc) {
	if fn != nil {
		fi.randFunc = fn
	}
}

// ShouldInject reports whether a random fault should be injected for the specified rate.
// If rate <= 0, it falls back to the configured defaultRate.
func (fi *FaultInjector) ShouldInject(rate float64) bool {
	targetRate := rate
	if targetRate <= 0.0 {
		targetRate = fi.defaultRate
	}

	if targetRate <= 0.0 || math.IsNaN(targetRate) || math.IsInf(targetRate, 0) {
		return false
	}
	if targetRate >= 1.0 {
		return true
	}

	fn := fi.randFunc
	if fn == nil {
		fn = rand.Float64
	}

	return fn() < targetRate
}

// GlobalShouldInject provides a package-level lock-free fault evaluator using math/rand/v2.
func GlobalShouldInject(rate float64) bool {
	if rate <= 0.0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return false
	}
	if rate >= 1.0 {
		return true
	}
	return rand.Float64() < rate
}
