package solarman

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
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
	if err := writeFull(t.conn, request); err != nil {
		t.close()
		return nil, fmt.Errorf("write Solarman request: %w", err)
	}

	header := make([]byte, v5HeaderSize)
	if _, err := io.ReadFull(t.conn, header); err != nil {
		t.close()
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
		return nil, fmt.Errorf("read Solarman payload: %w", err)
	}
	return frame, nil
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
