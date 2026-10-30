package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Keep stdin open until responses to already-read requests have been written.
type stdioPending struct {
	mu       sync.Mutex
	requests map[string]int
	changed  chan struct{}
}

func newStdioPending() *stdioPending {
	return &stdioPending{requests: map[string]int{}, changed: make(chan struct{}, 1)}
}
func (p *stdioPending) update(line []byte, incoming bool) {
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(line, &message) != nil || len(message.ID) == 0 || string(message.ID) == "null" {
		return
	}
	if incoming != (message.Method != "") {
		return
	}
	var id any
	if json.Unmarshal(message.ID, &id) != nil {
		return
	}
	key, e := json.Marshal(id)
	if e != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if incoming {
		p.requests[string(key)]++
	} else if p.requests[string(key)] > 1 {
		p.requests[string(key)]--
	} else {
		delete(p.requests, string(key))
	}
	select {
	case p.changed <- struct{}{}:
	default:
	}
}
func (p *stdioPending) wait(ctx context.Context) {
	for {
		p.mu.Lock()
		empty := len(p.requests) == 0
		p.mu.Unlock()
		if empty {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-p.changed:
		}
	}
}

type stdioFrames struct {
	pending   *stdioPending
	buffer    []byte
	oversized bool
	incoming  bool
}

func (f *stdioFrames) add(chunk []byte) {
	for len(chunk) > 0 {
		index := bytes.IndexByte(chunk, '\n')
		part := chunk
		if index >= 0 {
			part = chunk[:index]
		}
		if !f.oversized {
			if len(f.buffer)+len(part) > mcp.DefaultMaxLineLength {
				f.buffer = nil
				f.oversized = true
			} else {
				f.buffer = append(f.buffer, part...)
			}
		}
		if index < 0 {
			return
		}
		if !f.oversized {
			f.pending.update(f.buffer, f.incoming)
		}
		f.buffer = f.buffer[:0]
		f.oversized = false
		chunk = chunk[index+1:]
	}
}

type stdioReader struct {
	ctx      context.Context
	source   io.Reader
	frames   stdioFrames
	finished bool
}

func (r *stdioReader) Read(b []byte) (int, error) {
	if r.finished {
		r.frames.pending.wait(r.ctx)
		return 0, io.EOF
	}
	n, err := r.source.Read(b)
	if n > 0 {
		r.frames.add(b[:n])
	}
	if err == io.EOF {
		if len(r.frames.buffer) > 0 && !r.frames.oversized {
			r.frames.pending.update(r.frames.buffer, true)
			r.frames.buffer = nil
		}
		r.finished = true
		if n > 0 {
			return n, nil
		}
		r.frames.pending.wait(r.ctx)
	}
	return n, err
}
func (r *stdioReader) Close() error { return nil }

type stdioWriter struct {
	destination io.Writer
	frames      stdioFrames
}

func (w *stdioWriter) Write(b []byte) (int, error) {
	n, err := w.destination.Write(b)
	if n > 0 {
		w.frames.add(b[:n])
	}
	return n, err
}
func (w *stdioWriter) Close() error { return nil }
func stdioTransport(ctx context.Context, reader io.Reader, writer io.Writer) *mcp.IOTransport {
	pending := newStdioPending()
	return &mcp.IOTransport{Reader: &stdioReader{ctx: ctx, source: reader, frames: stdioFrames{pending: pending, incoming: true}}, Writer: &stdioWriter{destination: writer, frames: stdioFrames{pending: pending}}}
}
