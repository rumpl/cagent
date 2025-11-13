package pubsub

type Publisher[T any] interface {
	Subscribe(topic string, ch chan<- T)
	Unsubscribe(topic string, ch chan<- T)
	Publish(topic string, msg T)
}

type SimplePublisher[T any] struct {
	subscribers map[string][]chan<- T
}

func NewSimplePublisher[T any]() *SimplePublisher[T] {
	return &SimplePublisher[T]{
		subscribers: make(map[string][]chan<- T),
	}
}

func (p *SimplePublisher[T]) Subscribe(topic string, ch chan<- T) {
	p.subscribers[topic] = append(p.subscribers[topic], ch)
}

func (p *SimplePublisher[T]) Unsubscribe(topic string, ch chan<- T) {
	subs := p.subscribers[topic]
	for i, subscriber := range subs {
		if subscriber == ch {
			p.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func (p *SimplePublisher[T]) Publish(topic string, msg T) {
	for _, ch := range p.subscribers[topic] {
		ch <- msg
	}
}
