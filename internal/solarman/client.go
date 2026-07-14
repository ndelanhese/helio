package solarman

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type RegisterReader interface {
	ReadHoldingRegisters(context.Context, byte, uint16, uint16) ([]uint16, error)
}

type RoundTripper interface {
	RoundTrip(context.Context, []byte) ([]byte, error)
}

type Dialer func(context.Context) (RoundTripper, error)

type Config struct {
	Address string
	Serial  uint32
	Timeout time.Duration
}

type Client struct {
	mu       sync.Mutex
	config   Config
	dialer   Dialer
	sequence uint16
	conn     RoundTripper
}

func NewClient(config Config, dialer Dialer) *Client {
	if dialer == nil {
		dialer = func(ctx context.Context) (RoundTripper, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", config.Address)
			if err != nil {
				return nil, err
			}
			return newTCPRoundTripper(conn), nil
		}
	}
	return &Client{config: config, dialer: dialer}
}

func (c *Client) ReadHoldingRegisters(ctx context.Context, slave byte, start, count uint16) ([]uint16, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	requestCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := requestCtx.Err(); err != nil {
			return nil, err
		}
		sequence := c.nextSequence()
		request, err := BuildReadRequest(c.config.Serial, sequence, slave, start, count)
		if err != nil {
			return nil, err
		}
		if c.conn == nil {
			c.conn, err = c.dialer(requestCtx)
			if err != nil {
				lastErr = err
				continue
			}
		}
		response, err := c.conn.RoundTrip(requestCtx, request)
		if err == nil {
			var registers []uint16
			registers, err = ParseReadResponse(response, c.config.Serial, sequence)
			if err == nil {
				return registers, nil
			}
		}
		if !retryable(err) {
			return nil, err
		}
		lastErr = err
		c.invalidateConnection()
	}
	return nil, lastErr
}

func (c *Client) nextSequence() uint16 {
	c.sequence++
	if c.sequence == 0 {
		c.sequence++
	}
	return c.sequence
}

func (c *Client) invalidateConnection() {
	if closer, ok := c.conn.(io.Closer); ok {
		_ = closer.Close()
	}
	c.conn = nil
}

func retryable(err error) bool {
	return err != nil &&
		!errors.Is(err, ErrIdentityMismatch) &&
		!errors.Is(err, ErrUnsupportedFunction) &&
		!errors.Is(err, ErrModbusException)
}
