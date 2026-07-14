package solarman

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(context.Context, []byte) ([]byte, error)

func (f roundTripFunc) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	return f(ctx, request)
}

type serializingConn struct {
	mu      sync.Mutex
	active  int
	max     int
	writes  int
	failOne bool
}

func (f *serializingConn) RoundTrip(_ context.Context, request []byte) ([]byte, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.max {
		f.max = f.active
	}
	f.writes++
	fail := f.failOne
	f.failOne = false
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	if fail {
		return nil, io.EOF
	}
	time.Sleep(time.Millisecond)
	return responseForRequest(request), nil
}

func TestClientSerializesAndReconnectsOnce(t *testing.T) {
	conn := &serializingConn{failOne: true}
	var dials int
	client := NewClient(Config{Serial: 123456789, Timeout: time.Second}, func(context.Context) (RoundTripper, error) {
		dials++
		return conn, nil
	})

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1); err != nil {
				t.Errorf("ReadHoldingRegisters: %v", err)
			}
		}()
	}
	wg.Wait()

	if conn.max != 1 {
		t.Fatalf("max concurrent requests = %d, want 1", conn.max)
	}
	if conn.writes != 3 {
		t.Fatalf("round trips = %d, want 3", conn.writes)
	}
	if dials != 2 {
		t.Fatalf("dials = %d, want reconnect once", dials)
	}
}

func TestClientUsesTimeoutAndNonzeroSequences(t *testing.T) {
	var sequences []uint16
	client := NewClient(Config{Serial: 123456789, Timeout: 50 * time.Millisecond}, func(context.Context) (RoundTripper, error) {
		return roundTripFunc(func(ctx context.Context, request []byte) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 50*time.Millisecond || time.Until(deadline) <= 0 {
				t.Fatalf("unexpected deadline %v, ok=%v", deadline, ok)
			}
			sequences = append(sequences, binary.LittleEndian.Uint16(request[5:7]))
			return responseForRequest(request), nil
		}), nil
	})

	for range 2 {
		if _, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1); err != nil {
			t.Fatal(err)
		}
	}
	if len(sequences) != 2 || sequences[0] == 0 || sequences[1] == 0 || sequences[0] == sequences[1] {
		t.Fatalf("sequences = %v, want distinct nonzero values", sequences)
	}
}

func TestClientTimeoutIncludesWaitingForSerialization(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	client := NewClient(Config{Serial: 123456789, Timeout: 50 * time.Millisecond}, func(context.Context) (RoundTripper, error) {
		return roundTripFunc(func(_ context.Context, request []byte) ([]byte, error) {
			once.Do(func() { close(entered) })
			<-release
			return responseForRequest(request), nil
		}), nil
	})

	firstDone := make(chan error, 1)
	go func() {
		_, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1)
		firstDone <- err
	}()
	<-entered

	started := time.Now()
	queuedDone := make(chan error, 1)
	go func() {
		_, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1)
		queuedDone <- err
	}()
	select {
	case err := <-queuedDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("queued error = %v, want context.DeadlineExceeded", err)
		}
		if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
			t.Fatalf("queued call returned after %v, want prompt timeout", elapsed)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		<-firstDone
		t.Fatal("queued call did not time out while waiting for serialization")
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first call: %v", err)
	}
}

func TestClientRetryClassification(t *testing.T) {
	tests := []struct {
		name      string
		first     func([]byte) ([]byte, error)
		wantCalls int
		wantErr   error
	}{
		{name: "I/O", first: func([]byte) ([]byte, error) { return nil, io.ErrUnexpectedEOF }, wantCalls: 2},
		{name: "CRC", first: func(req []byte) ([]byte, error) {
			frame := responseForRequest(req)
			frame[len(frame)-4]++
			refreshFrameChecksum(frame)
			return frame, nil
		}, wantCalls: 2},
		{name: "malformed", first: func([]byte) ([]byte, error) { return []byte{0xA5}, nil }, wantCalls: 2},
		{name: "identity", first: func(req []byte) ([]byte, error) {
			frame := responseForRequest(req)
			binary.LittleEndian.PutUint32(frame[7:11], 1)
			refreshFrameChecksum(frame)
			return frame, nil
		}, wantCalls: 1, wantErr: ErrIdentityMismatch},
		{name: "unsupported function", first: func(req []byte) ([]byte, error) {
			return responseForRequestWithModbus(req, []byte{1, 6, 0, 0, 0, 1}), nil
		}, wantCalls: 1, wantErr: ErrUnsupportedFunction},
		{name: "Modbus exception", first: func(req []byte) ([]byte, error) {
			return responseForRequestWithModbus(req, []byte{1, 0x83, 2}), nil
		}, wantCalls: 1, wantErr: ErrModbusException},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := NewClient(Config{Serial: 123456789, Timeout: time.Second}, func(context.Context) (RoundTripper, error) {
				return roundTripFunc(func(_ context.Context, request []byte) ([]byte, error) {
					calls++
					if calls == 1 {
						return tt.first(request)
					}
					return responseForRequest(request), nil
				}), nil
			})
			_, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func responseForRequest(request []byte) []byte {
	return responseForRequestWithModbus(request, []byte{1, 3, 2, 0x12, 0x34})
}

func responseForRequestWithModbus(request, modbus []byte) []byte {
	withCRC := append([]byte(nil), modbus...)
	crc := CRC16(withCRC)
	withCRC = append(withCRC, byte(crc), byte(crc>>8))
	payload := append([]byte{2, 1}, make([]byte, 12)...)
	payload = append(payload, withCRC...)
	return buildFrame(v5ResponseControl, binary.LittleEndian.Uint32(request[7:11]), binary.LittleEndian.Uint16(request[5:7]), payload)
}
