// Package ssestream decodes a text/event-stream response body into a typed
// stream of events.
package ssestream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Event is one server-sent event.
type Event struct {
	Type string
	Data []byte
}

// Decoder yields the raw events of a stream.
type Decoder interface {
	Event() Event
	Next() bool
	Close() error
	Err() error
}

var decoderTypes = map[string]func(io.ReadCloser) Decoder{}

// RegisterDecoder installs a decoder for a non-SSE content type.
func RegisterDecoder(contentType string, decoder func(io.ReadCloser) Decoder) {
	decoderTypes[strings.ToLower(contentType)] = decoder
}

// NewDecoder returns a Decoder reading res's body, or nil when there is none.
func NewDecoder(res *http.Response) Decoder {
	if res == nil || res.Body == nil {
		return nil
	}
	if newDecoder, ok := decoderTypes[strings.ToLower(res.Header.Get("content-type"))]; ok {
		return newDecoder(res.Body)
	}
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(nil, bufio.MaxScanTokenSize<<9)
	return &eventStreamDecoder{rc: res.Body, scn: scanner}
}

type eventStreamDecoder struct {
	evt Event
	rc  io.ReadCloser
	scn *bufio.Scanner
	err error
}

func (s *eventStreamDecoder) Next() bool {
	if s.err != nil {
		return false
	}

	event := ""
	var data []byte

	for s.scn.Scan() {
		txt := s.scn.Bytes()

		// An empty line dispatches the event accumulated so far.
		if len(txt) == 0 {
			if len(data) == 0 {
				event = ""
				continue
			}
			s.evt = Event{Type: event, Data: data}
			return true
		}

		name, value, _ := bytes.Cut(txt, []byte(":"))
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		switch string(name) {
		case "":
			// ": comment" lines carry no data and must be ignored.
			continue
		case "event":
			event = string(value)
		case "data":
			data = append(data, value...)
			data = append(data, '\n')
		}
	}

	if s.scn.Err() != nil {
		s.err = s.scn.Err()
	}
	return false
}

func (s *eventStreamDecoder) Event() Event { return s.evt }
func (s *eventStreamDecoder) Close() error { return s.rc.Close() }
func (s *eventStreamDecoder) Err() error   { return s.err }

// StreamError is returned when the stream carries an error payload.
type StreamError struct {
	Message string
	Event   Event
}

func (e *StreamError) Error() string { return e.Message }

// Stream decodes the events of a Decoder into values of type T.
type Stream[T any] struct {
	decoder Decoder
	cur     T
	err     error
	done    bool
}

// NewStream returns a stream over decoder. A non-nil err is reported by
// [Stream.Err] and makes [Stream.Next] return false immediately, so request
// errors can be surfaced without a separate return value.
func NewStream[T any](decoder Decoder, err error) *Stream[T] {
	return &Stream[T]{decoder: decoder, err: err}
}

// Next advances to the next event, reporting false at end of stream or on error.
func (s *Stream[T]) Next() bool {
	if s.err != nil || s.decoder == nil {
		return false
	}

	for s.decoder.Next() {
		if s.done {
			continue
		}

		data := s.decoder.Event().Data
		if bytes.HasPrefix(data, []byte("[DONE]")) {
			// Keep draining so the connection can be reused, but stop decoding.
			s.done = true
			continue
		}

		if raw, ok := errorPayload(data); ok {
			s.err = &StreamError{
				Message: "received error while streaming: " + raw,
				Event:   s.decoder.Event(),
			}
			return false
		}

		var next T
		if s.err = json.Unmarshal(data, &next); s.err != nil {
			return false
		}
		s.cur = next
		return true
	}

	s.err = s.decoder.Err()
	return false
}

func (s *Stream[T]) Current() T { return s.cur }
func (s *Stream[T]) Err() error { return s.err }

func (s *Stream[T]) Close() error {
	if s.decoder == nil {
		return nil
	}
	return s.decoder.Close()
}

// errorPayload reports whether the event data carries a top-level "error"
// member, returning it rendered the way the API surfaces it (a JSON string is
// unquoted, anything else is kept verbatim).
func errorPayload(data []byte) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", false
	}
	raw, ok := obj["error"]
	if !ok {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, true
	}
	return string(raw), true
}
