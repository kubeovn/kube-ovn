package framework

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type EventClient struct {
	f *Framework
	typedcorev1.EventInterface
}

func (f *Framework) EventClient() *EventClient {
	return f.EventClientNS(f.Namespace.Name)
}

func (f *Framework) EventClientNS(namespace string) *EventClient {
	return &EventClient{
		f:              f,
		EventInterface: f.ClientSet.CoreV1().Events(namespace),
	}
}

// WaitToHaveEvent waits the provided resource to have the specified event(s)
func (c *EventClient) WaitToHaveEvent(kind, name, eventType, reason, sourceComponent, sourceHost string) []corev1.Event {
	var result []corev1.Event
	err := wait.Poll(poll, timeout, func() (bool, error) {
		Logf("Waiting for %s %s/%s to have event %s/%s", kind, c.f.Namespace.Name, name, eventType, reason)
		selector := fields.Set{
			"involvedObject.kind": kind,
			"involvedObject.name": name,
			"type":                eventType,
			"reason":              reason,
		}

		events, err := c.List(context.TODO(), metav1.ListOptions{FieldSelector: selector.AsSelector().String()})
		if err != nil {
			return handleWaitingAPIError(err, true, "listing events")
		}
		for _, event := range events.Items {
			if sourceComponent != "" && event.Source.Component != sourceComponent {
				continue
			}
			if sourceHost != "" && event.Source.Host != sourceHost {
				continue
			}
			result = append(result, event)
		}
		return len(result) != 0, nil
	})

	ExpectNoError(err)
	return result
}
