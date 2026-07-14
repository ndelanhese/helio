package solarman

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type scriptedConn struct {
	reader       io.Reader
	written      bytes.Buffer
	deadline     time.Time
	closed       bool
	setDeadlineE error
}

func (c *scriptedConn) Read(p []byte) (int, error)       { return c.reader.Read(p) }
func (c *scriptedConn) Write(p []byte) (int, error)      { return c.written.Write(p) }
func (c *scriptedConn) Close() error                     { c.closed = true; return nil }
func (c *scriptedConn) LocalAddr() net.Addr              { return stubAddr("local") }
func (c *scriptedConn) RemoteAddr() net.Addr             { return stubAddr("remote") }
func (c *scriptedConn) SetDeadline(t time.Time) error    { c.deadline = t; return c.setDeadlineE }
func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }

type stubAddr string

func (a stubAddr) Network() string { return "tcp" }
func (a stubAddr) String() string  { return string(a) }

func TestTCPRoundTripReadsFragmentedFrameAndSetsDeadline(t *testing.T) {
	response := fixture(t, "read_holding_response.hex")
	conn := &scriptedConn{reader: &oneByteReader{data: response}}
	transport := newTCPRoundTripper(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := []byte{1, 2, 3}

	got, err := transport.RoundTrip(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("response = %x, want %x", got, response)
	}
	if !bytes.Equal(conn.written.Bytes(), request) {
		t.Fatalf("request = %x, want %x", conn.written.Bytes(), request)
	}
	if conn.deadline.IsZero() {
		t.Fatal("deadline was not set")
	}
	if conn.closed {
		t.Fatal("successful connection was closed")
	}
}

func TestTCPRoundTripClosesAfterPartialRead(t *testing.T) {
	response := fixture(t, "read_holding_response.hex")
	conn := &scriptedConn{reader: bytes.NewReader(response[:v5HeaderSize+1])}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := newTCPRoundTripper(conn).RoundTrip(ctx, []byte{1})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want io.ErrUnexpectedEOF", err)
	}
	if !conn.closed {
		t.Fatal("connection not closed after partial read")
	}
}

func TestTCPRoundTripRejectsOversizedFrame(t *testing.T) {
	header := make([]byte, v5HeaderSize)
	header[0] = v5Start
	binary.LittleEndian.PutUint16(header[1:3], 500)
	conn := &scriptedConn{reader: bytes.NewReader(header)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := newTCPRoundTripper(conn).RoundTrip(ctx, []byte{1})
	if !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("error = %v, want ErrMalformedFrame", err)
	}
	if !conn.closed {
		t.Fatal("connection not closed after oversized header")
	}
}

func TestTCPRoundTripRequiresContextDeadline(t *testing.T) {
	conn := &scriptedConn{reader: bytes.NewReader(nil)}
	_, err := newTCPRoundTripper(conn).RoundTrip(context.Background(), []byte{1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

type oneByteReader struct{ data []byte }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}
