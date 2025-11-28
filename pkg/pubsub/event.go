package pubsub

// Event wraps any payload
type Event[T any] struct {
	Payload T
}
