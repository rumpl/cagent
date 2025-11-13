package pubsub

import "time"

type Event[T any] struct {
	Topic string
	Time  time.Time
	Data  T
}
