package contract

// EventSink receives runtime events and must be safe for concurrent use.
type EventSink interface{ Emit(event Event) }
