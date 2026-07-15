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
	deadlineSet  chan time.Time
	closed       bool
	setDeadlineE error
}

func (c *scriptedConn) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *scriptedConn) Write(p []byte) (int, error) { return c.written.Write(p) }
func (c *scriptedConn) Close() error                { c.closed = true; return nil }
func (c *scriptedConn) LocalAddr() net.Addr         { return stubAddr("local") }
func (c *scriptedConn) RemoteAddr() net.Addr        { return stubAddr("remote") }
func (c *scriptedConn) SetDeadline(t time.Time) error {
	c.deadline = t
	if c.deadlineSet != nil {
		c.deadlineSet <- t
	}
	return c.setDeadlineE
}
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

func TestTCPRoundTripAnswersLoggerHeartbeatBeforeModbusResponse(t *testing.T) {
	request := []byte{1, 2, 3}
	heartbeat := buildFrame(0x4710, 123456789, 0x2207, make([]byte, 10))
	response := fixture(t, "read_holding_response.hex")
	conn := &scriptedConn{reader: bytes.NewReader(append(heartbeat, response...))}
	transport := newTCPRoundTripper(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := transport.RoundTrip(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response) {
		t.Fatalf("response = %x, want %x", got, response)
	}
	written := conn.written.Bytes()
	if !bytes.HasPrefix(written, request) {
		t.Fatalf("writes do not begin with request: %x", written)
	}
	heartbeatReply := written[len(request):]
	if heartbeatReply[3] != 0x10 || heartbeatReply[4] != 0x17 {
		t.Fatalf("heartbeat reply control = %02x %02x, want 10 17", heartbeatReply[3], heartbeatReply[4])
	}
	if heartbeatReply[5] != 0x08 || heartbeatReply[6] != 0x22 {
		t.Fatalf("heartbeat reply sequence = %02x %02x, want 08 22", heartbeatReply[5], heartbeatReply[6])
	}
	if binary.LittleEndian.Uint32(heartbeatReply[7:11]) != 123456789 {
		t.Fatalf("heartbeat reply logger serial mismatch")
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

func TestTCPRoundTripCancellationInterruptsBlockedRead(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	readStarted := make(chan struct{})
	go func() {
		var request [1]byte
		_, _ = io.ReadFull(serverConn, request[:])
		close(readStarted)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	done := make(chan error, 1)
	go func() {
		_, err := newTCPRoundTripper(clientConn).RoundTrip(ctx, []byte{1})
		done <- err
	}()
	<-readStarted

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("RoundTrip did not return promptly after cancellation")
	}
}

func TestTCPRoundTripStopsCancellationCallbackAfterSuccess(t *testing.T) {
	response := fixture(t, "read_holding_response.hex")
	conn := &scriptedConn{reader: bytes.NewReader(response), deadlineSet: make(chan time.Time, 2)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	if _, err := newTCPRoundTripper(conn).RoundTrip(ctx, []byte{1}); err != nil {
		t.Fatal(err)
	}
	<-conn.deadlineSet
	cancel()
	select {
	case deadline := <-conn.deadlineSet:
		t.Fatalf("late cancellation changed completed operation deadline to %v", deadline)
	case <-time.After(50 * time.Millisecond):
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
