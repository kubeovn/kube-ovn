package ovndbtls

import "time"

func NeedsRenewal(now, notBefore, notAfter time.Time) bool {
	lifetime := notAfter.Sub(notBefore)
	if lifetime <= 0 {
		return true
	}
	return !now.Before(notBefore.Add(lifetime / 2))
}

func stageReady(now, started time.Time, delay time.Duration) bool {
	return !now.Before(started.Add(delay))
}
