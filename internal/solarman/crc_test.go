package solarman

import (
	"encoding/hex"
	"testing"
)

func TestCRC16KnownModbusVector(t *testing.T) {
	b, err := hex.DecodeString("01030000000a")
	if err != nil {
		t.Fatal(err)
	}
	if got := CRC16(b); got != 0xCDC5 {
		t.Fatalf("got %04x", got)
	}
}
