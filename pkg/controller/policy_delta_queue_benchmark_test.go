package controller

import (
	"fmt"
	"testing"

	"k8s.io/client-go/util/workqueue"
)

// BenchmarkPolicyDeltaEventStorm compares the number of work items produced
// by a raw pointer queue with the coalescing queue for the same policy events.
// The benchmark measures queue pressure only; it does not claim to measure
// address-set writes or end-to-end reconcile time.
func BenchmarkPolicyDeltaEventStorm(b *testing.B) {
	fields := [...]ChangedField{ChangedSubject, ChangedIngressRule, ChangedEgressRule}
	for _, policyCount := range []int{1, 100} {
		for _, eventsPerPolicy := range []int{10, 100} {
			for _, coalescing := range []bool{false, true} {
				name := fmt.Sprintf("policies-%d/events-%d/coalescing-%t", policyCount, eventsPerPolicy, coalescing)
				b.Run(name, func(b *testing.B) {
					queuedItems := 0
					for b.Loop() {
						var queue workqueue.TypedRateLimitingInterface[*AdminNetworkPolicyChangedDelta]
						if coalescing {
							queue = newCoalescingDeltaQueue(
								"benchmark-policy-delta",
								func(delta *AdminNetworkPolicyChangedDelta) string { return delta.key },
								mergeAdminNetworkPolicyDelta,
							)
						} else {
							queue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("benchmark-policy-delta", nil)
						}
						for policy := range policyCount {
							for event := range eventsPerPolicy {
								queue.Add(&AdminNetworkPolicyChangedDelta{
									key:   fmt.Sprintf("policy-%d", policy),
									field: fields[event%len(fields)],
								})
							}
						}
						queuedItems += queue.Len()
						queue.ShutDown()
					}
					b.ReportMetric(float64(queuedItems)/float64(b.N), "queued-items/op")
				})
			}
		}
	}
}
