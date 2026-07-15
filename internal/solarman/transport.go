package solarman

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const maxFrameSize = 512

type tcpRoundTripper struct {
	conn net.Conn
}

func newTCPRoundTripper(conn net.Conn) *tcpRoundTripper {
	return &tcpRoundTripper{conn: conn}
}

func (t *tcpRoundTripper) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, context.DeadlineExceeded
	}
	if err := t.conn.SetDeadline(deadline); err != nil {
		t.close()
		return nil, fmt.Errorf("set TCP deadline: %w", err)
	}
	cancelDone := make(chan struct{})
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = t.conn.SetDeadline(time.Now())
		close(cancelDone)
	})
	defer func() {
		if !stopCancellation() {
			<-cancelDone
		}
	}()
	if err := writeFull(t.conn, request); err != nil {
		t.close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("write Solarman request: %w", err)
	}

	for {
		frame, err := t.readFrame(ctx)
		if err != nil {
			return nil, err
		}
		if !isLoggerProtocolFrame(frame) {
			return frame, nil
		}
		if err := writeFull(t.conn, buildLoggerTimeResponse(frame, time.Now())); err != nil {
			t.close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("write Solarman protocol response: %w", err)
		}
	}
}

func (t *tcpRoundTripper) readFrame(ctx context.Context) ([]byte, error) {
	header := make([]byte, v5HeaderSize)
	if _, err := io.ReadFull(t.conn, header); err != nil {
		t.close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read Solarman header: %w", err)
	}
	payloadSize := int(binary.LittleEndian.Uint16(header[1:3]))
	frameSize := v5HeaderSize + payloadSize + v5TrailerSize
	if frameSize > maxFrameSize {
		t.close()
		return nil, fmt.Errorf("%w: declared frame size %d exceeds %d", ErrMalformedFrame, frameSize, maxFrameSize)
	}

	frame := make([]byte, frameSize)
	copy(frame, header)
	if _, err := io.ReadFull(t.conn, frame[v5HeaderSize:]); err != nil {
		t.close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read Solarman payload: %w", err)
	}
	return frame, nil
}

func isLoggerProtocolFrame(frame []byte) bool {
	if len(frame) < v5HeaderSize+v5TrailerSize || frame[0] != v5Start || frame[len(frame)-1] != v5End || additiveChecksum(frame[1:len(frame)-2]) != frame[len(frame)-2] || frame[3] != 0x10 {
		return false
	}
	switch frame[4] {
	case 0x41, 0x42, 0x43, 0x47, 0x48:
		return true
	default:
		return false
	}
}

func buildLoggerTimeResponse(request []byte, now time.Time) []byte {
	sequence := binary.LittleEndian.Uint16(request[5:7])
	sequence = (sequence & 0xff00) | uint16(byte(request[5]+1))
	payload := make([]byte, 10)
	binary.LittleEndian.PutUint16(payload[:2], 0x0100)
	binary.LittleEndian.PutUint32(payload[2:6], uint32(now.Unix()))
	control := uint16(request[4]-0x30)<<8 | 0x10
	return buildFrame(control, binary.LittleEndian.Uint32(request[7:11]), sequence, payload)
}

func (t *tcpRoundTripper) Close() error {
	return t.conn.Close()
}

func (t *tcpRoundTripper) close() {
	_ = t.conn.Close()
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
