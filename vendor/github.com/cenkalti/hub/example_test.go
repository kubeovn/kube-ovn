package hub_test

import (
	"fmt"

	"github.com/cenk/hub"
)

// Different event kinds
const (
	happenedA hub.Kind = iota
	happenedB
	happenedC
)

// Our custom event type
type EventA struct {
	arg1, arg2 int
}

// Implement hub.Event interface
func (e EventA) Kind() hub.Kind { return happenedA }

func Example() {
	hub.Subscribe(happenedA, func(e hub.Event) {
		a := e.(EventA) // Cast to concrete type
		fmt.Println(a.arg1 + a.arg2)
	})

	hub.Publish(EventA{2, 3})
	// Output: 5
}
