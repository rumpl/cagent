// Package ssestream decodes a text/event-stream response into a typed stream
// of events.
package ssestream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/internal/apierror"
)

// Event is a single server-sent event.
type Event struct {
	Type string
	Data []byte
}

// Decoder yields the events of an SSE response.
type Decoder interface {
	Event() Event
	Next() bool
	Close() error
	Err() error
}

var decoderTypes = map[string](func(io.ReadCloser) Decoder){}

// RegisterDecoder installs a decoder for a non-standard content type.
func RegisterDecoder(contentType string, decoder func(io.ReadCloser) Decoder) {
	decoderTypes[strings.ToLower(contentType)] = decoder
}

// NewDecoder returns a decoder over res, or nil when there is no body.
func NewDecoder(res *http.Response) Decoder {
	if res == nil || res.Body == nil {
		return nil
	}

	var decoder Decoder
	if t, ok := decoderTypes[res.Header.Get("content-type")]; ok {
		decoder = t(res.Body)
	} else {
		scn := bufio.NewScanner(res.Body)
		scn.Buffer(nil, bufio.MaxScanTokenSize<<9)
		decoder = &eventStreamDecoder{rc: res.Body, scn: scn}
	}

	// The response is needed to build rich errors from in-band error events.
	if res.Request != nil {
		return richErrorDecoder{Decoder: decoder, resp: res}
	}
	return decoder
}

// richErrorDecoder carries the response so an in-band `error` event can be
// turned into the same *apierror.Error a non-2xx response would produce.
type richErrorDecoder struct {
	Decoder
	resp *http.Response
}

func (d *richErrorDecoder) newAPIError(errorJSON []byte) error {
	aerr := &apierror.Error{}
	if d.resp != nil {
		aerr.Request = d.resp.Request
		aerr.Response = d.resp
		aerr.StatusCode = d.resp.StatusCode
		aerr.RequestID = d.resp.Header.Get("request-id")
		aerr.WorkspaceID = d.resp.Header.Get("anthropic-workspace-id")
	}
	if aerr.UnmarshalJSON(errorJSON) != nil {
		return fmt.Errorf("received error while streaming: %s", string(errorJSON))
	}
	return aerr
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
	data := bytes.NewBuffer(nil)

	for s.scn.Scan() {
		txt := s.scn.Bytes()

		// An empty line dispatches the accumulated event.
		if len(txt) == 0 {
			s.evt = Event{Type: event, Data: data.Bytes()}
			return true
		}

		name, value, _ := bytes.Cut(txt, []byte(":"))
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		switch string(name) {
		case "":
			continue // comment line
		case "event":
			event = string(value)
		case "data":
			// Writes to a bytes.Buffer never fail.
			data.Write(value)
			data.WriteRune('\n')
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

// Stream decodes each data payload of an SSE response into a T.
type Stream[T any] struct {
	decoder Decoder
	cur     T
	err     error
}

// NewStream returns a stream over decoder. A non-nil err short-circuits it,
// which is how request failures are reported to the caller.
func NewStream[T any](decoder Decoder, err error) *Stream[T] {
	return &Stream[T]{decoder: decoder, err: err}
}

// Next advances the stream, returning false at the end or on error.
func (s *Stream[T]) Next() bool {
	if s.err != nil {
		return false
	}

	for s.decoder.Next() {
		switch s.decoder.Event().Type {
		case "ping":
			continue
		case "error":
			data := s.decoder.Event().Data
			if ed, ok := s.decoder.(richErrorDecoder); ok {
				s.err = ed.newAPIError(data)
			} else {
				s.err = fmt.Errorf("received error while streaming: %s", string(data))
			}
			return false
		case "completion", "message", "message_start", "message_delta", "message_stop",
			"content_block_start", "content_block_delta", "content_block_stop":
			var nxt T
			if s.err = json.Unmarshal(s.decoder.Event().Data, &nxt); s.err != nil {
				return false
			}
			s.cur = nxt
			return true
		}
	}

	// decoder.Next() may be false because of an error.
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
