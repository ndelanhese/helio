package solarman

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
)

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hex.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBuildReadRequest(t *testing.T) {
	got, err := BuildReadRequest(123456789, 7, 1, 0x0000, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := fixture(t, "read_holding_request.hex")
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("got %x want %x", got, want)
	}
}

func TestBuildReadRequestRejectsInvalidCount(t *testing.T) {
	for _, count := range []uint16{0, 126} {
		if _, err := BuildReadRequest(123456789, 7, 1, 0, count); !errors.Is(err, ErrMalformedFrame) {
			t.Fatalf("count %d: got %v, want ErrMalformedFrame", count, err)
		}
	}
}

func TestParseReadResponse(t *testing.T) {
	got, err := ParseReadResponse(fixture(t, "read_holding_response.hex"), 123456789, 7, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 0x1234 {
		t.Fatalf("got %04x, want [1234]", got)
	}
}

func TestParseRejectsResponseForDifferentModbusRequest(t *testing.T) {
	for name, frame := range map[string][]byte{
		"slave": responseFrame([]byte{2, 3, 2, 0x12, 0x34}),
		"count": responseFrame([]byte{1, 3, 4, 0x12, 0x34, 0x56, 0x78}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrIdentityMismatch) {
				t.Fatalf("got %v, want ErrIdentityMismatch", err)
			}
		})
	}
}

func TestParseRejectsWrongSerialAndWriteFunction(t *testing.T) {
	frame := fixture(t, "read_holding_response.hex")
	if _, err := ParseReadResponse(frame, 1, 7, 1, 1); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("got %v, want ErrIdentityMismatch", err)
	}

	frame = fixture(t, "read_holding_response.hex")
	frame[len(frame)-8] = 0x06
	refreshChecksums(frame)
	if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrUnsupportedFunction) {
		t.Fatalf("got %v, want ErrUnsupportedFunction", err)
	}
}

func TestParseRejectsMalformedEnvelope(t *testing.T) {
	tests := map[string]func([]byte) []byte{
		"start marker": func(frame []byte) []byte { frame[0] = 0; return frame },
		"end marker":   func(frame []byte) []byte { frame[len(frame)-1] = 0; return frame },
		"control": func(frame []byte) []byte {
			frame[3] = 0x45
			refreshFrameChecksum(frame)
			return frame
		},
		"length": func(frame []byte) []byte {
			binary.LittleEndian.PutUint16(frame[1:3], 1)
			refreshFrameChecksum(frame)
			return frame
		},
		"checksum": func(frame []byte) []byte { frame[len(frame)-2]++; return frame },
		"trailing bytes": func(frame []byte) []byte {
			return responseFrame([]byte{1, 3, 2, 0x12, 0x34, 0})
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			frame := mutate(fixture(t, "read_holding_response.hex"))
			if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrMalformedFrame) {
				t.Fatalf("got %v, want ErrMalformedFrame", err)
			}
		})
	}
}

func TestParseRejectsWrongSequence(t *testing.T) {
	if _, err := ParseReadResponse(fixture(t, "read_holding_response.hex"), 123456789, 8, 1, 1); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("got %v, want ErrIdentityMismatch", err)
	}
}

func TestParseRejectsBadModbusCRC(t *testing.T) {
	frame := fixture(t, "read_holding_response.hex")
	frame[len(frame)-4]++
	refreshFrameChecksum(frame)
	if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrCRC) {
		t.Fatalf("got %v, want ErrCRC", err)
	}
}

func TestParseChecksCRCBeforeFunctionAndByteCount(t *testing.T) {
	tests := map[string]func([]byte){
		"function":   func(frame []byte) { frame[len(frame)-8] = 0x06 },
		"byte count": func(frame []byte) { frame[len(frame)-7] = 1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			frame := fixture(t, "read_holding_response.hex")
			mutate(frame)
			refreshFrameChecksum(frame)
			if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrCRC) {
				t.Fatalf("got %v, want ErrCRC", err)
			}
		})
	}
}

func TestParseRejectsModbusException(t *testing.T) {
	t.Run("read holding exception", func(t *testing.T) {
		frame := responseFrame([]byte{1, 0x83, 2})
		if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrModbusException) {
			t.Fatalf("got %v, want ErrModbusException", err)
		}
	})
	t.Run("write exception", func(t *testing.T) {
		frame := responseFrame([]byte{1, 0x86, 2})
		if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrUnsupportedFunction) {
			t.Fatalf("got %v, want ErrUnsupportedFunction", err)
		}
	})
}

func TestParseRejectsOddByteCount(t *testing.T) {
	frame := fixture(t, "read_holding_response.hex")
	frame[len(frame)-7] = 1
	refreshChecksums(frame)
	if _, err := ParseReadResponse(frame, 123456789, 7, 1, 1); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("got %v, want ErrMalformedFrame", err)
	}
}

func TestParseRejectsMoreThan125Registers(t *testing.T) {
	modbus := append([]byte{1, 3, 252}, make([]byte, 252)...)
	if _, err := ParseReadResponse(responseFrame(modbus), 123456789, 7, 1, 1); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("got %v, want ErrMalformedFrame", err)
	}
}

func FuzzParseReadResponse(f *testing.F) {
	f.Add(fixture(f, "read_holding_response.hex"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseReadResponse(b, 123456789, 7, 1, 1)
	})
}

func responseFrame(modbus []byte) []byte {
	withCRC := append([]byte(nil), modbus...)
	crc := CRC16(withCRC)
	withCRC = append(withCRC, byte(crc), byte(crc>>8))
	payload := append([]byte{2, 1}, make([]byte, 12)...)
	payload = append(payload, withCRC...)
	frame := make([]byte, 11, 11+len(payload)+2)
	frame[0] = 0xA5
	binary.LittleEndian.PutUint16(frame[1:3], uint16(len(payload)))
	binary.LittleEndian.PutUint16(frame[3:5], 0x1015)
	binary.LittleEndian.PutUint16(frame[5:7], 7)
	binary.LittleEndian.PutUint32(frame[7:11], 123456789)
	frame = append(frame, payload...)
	frame = append(frame, 0, 0x15)
	refreshFrameChecksum(frame)
	return frame
}

func refreshChecksums(frame []byte) {
	modbus := frame[25 : len(frame)-2]
	crc := CRC16(modbus[:len(modbus)-2])
	modbus[len(modbus)-2] = byte(crc)
	modbus[len(modbus)-1] = byte(crc >> 8)
	refreshFrameChecksum(frame)
}

func refreshFrameChecksum(frame []byte) {
	var sum byte
	for _, b := range frame[1 : len(frame)-2] {
		sum += b
	}
	frame[len(frame)-2] = sum
}
