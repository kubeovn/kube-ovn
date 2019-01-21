package hub

import "testing"

const testKind Kind = 1
const testValue = "foo"

type testEvent string

func (e testEvent) Kind() Kind {
	return testKind
}

func TestPubSub(t *testing.T) {
	var h Hub
	var s string

	h.Subscribe(testKind, func(e Event) { s = string(e.(testEvent)) })
	h.Publish(testEvent(testValue))

	if s != testValue {
		t.Errorf("invalid value: %s", s)
	}
}

func TestCancel(t *testing.T) {
	var h Hub
	var called int
	var f = func(e Event) { called += 1 }

	_ = h.Subscribe(testKind, f)
	cancel := h.Subscribe(testKind, f)
	h.Publish(testEvent(testValue)) // 2 calls to f
	cancel()
	h.Publish(testEvent(testValue)) // 1 call to f

	if called != 3 {
		t.Errorf("unexpected call count: %d", called)
	}
}
