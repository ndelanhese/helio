package solarman

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	ErrMalformedFrame      = errors.New("malformed Solarman frame")
	ErrIdentityMismatch    = errors.New("Solarman frame identity mismatch")
	ErrUnsupportedFunction = errors.New("unsupported Modbus function")
	ErrModbusException     = errors.New("Modbus exception")
	ErrCRC                 = errors.New("Modbus CRC mismatch")
)

const (
	v5Start              = 0xA5
	v5End                = 0x15
	v5RequestControl     = 0x4510
	v5ResponseControl    = 0x1510
	v5HeaderSize         = 11
	v5TrailerSize        = 2
	requestMetadataSize  = 15
	responseMetadataSize = 14
	readHoldingFunction  = 0x03
	maxReadRegisters     = 125
)

// BuildReadRequest wraps a Modbus read-holding-registers request in a
// Solarman V5 client request frame.
func BuildReadRequest(serial uint32, sequence uint16, slave byte, start, count uint16) ([]byte, error) {
	if count == 0 || count > maxReadRegisters {
		return nil, fmt.Errorf("%w: register count %d outside 1..%d", ErrMalformedFrame, count, maxReadRegisters)
	}

	modbus := make([]byte, 8)
	modbus[0] = slave
	modbus[1] = readHoldingFunction
	binary.BigEndian.PutUint16(modbus[2:4], start)
	binary.BigEndian.PutUint16(modbus[4:6], count)
	binary.LittleEndian.PutUint16(modbus[6:8], CRC16(modbus[:6]))

	payload := make([]byte, requestMetadataSize, requestMetadataSize+len(modbus))
	payload[0] = 0x02
	payload = append(payload, modbus...)
	return buildFrame(v5RequestControl, serial, sequence, payload), nil
}

// ParseReadResponse validates a complete Solarman V5 client response and
// returns its read-only Modbus holding-register values.
func ParseReadResponse(frame []byte, expectedSerial uint32, expectedSequence uint16, expectedSlave byte, expectedCount uint16) ([]uint16, error) {
	minimumSize := v5HeaderSize + responseMetadataSize + 5 + v5TrailerSize
	if len(frame) < minimumSize {
		return nil, fmt.Errorf("%w: frame too short", ErrMalformedFrame)
	}
	if frame[0] != v5Start || frame[len(frame)-1] != v5End {
		return nil, fmt.Errorf("%w: bad frame marker", ErrMalformedFrame)
	}
	if int(binary.LittleEndian.Uint16(frame[1:3])) != len(frame)-v5HeaderSize-v5TrailerSize {
		return nil, fmt.Errorf("%w: payload length mismatch", ErrMalformedFrame)
	}
	if binary.LittleEndian.Uint16(frame[3:5]) != v5ResponseControl {
		return nil, fmt.Errorf("%w: invalid response control", ErrMalformedFrame)
	}
	if additiveChecksum(frame[1:len(frame)-2]) != frame[len(frame)-2] {
		return nil, fmt.Errorf("%w: frame checksum mismatch", ErrMalformedFrame)
	}
	if frame[5] != byte(expectedSequence) ||
		binary.LittleEndian.Uint32(frame[7:11]) != expectedSerial {
		return nil, ErrIdentityMismatch
	}

	payload := frame[v5HeaderSize : len(frame)-v5TrailerSize]
	if payload[0] != 0x02 || payload[1] != 0x01 {
		return nil, fmt.Errorf("%w: invalid response metadata", ErrMalformedFrame)
	}
	modbus := payload[responseMetadataSize:]
	if err := validateModbusCRC(modbus); err != nil {
		return nil, err
	}
	function := modbus[1]
	if modbus[0] != expectedSlave {
		return nil, ErrIdentityMismatch
	}
	if function == readHoldingFunction|0x80 {
		if len(modbus) != 5 {
			return nil, fmt.Errorf("%w: invalid exception length", ErrMalformedFrame)
		}
		return nil, fmt.Errorf("%w: function 0x%02x code 0x%02x", ErrModbusException, function&^0x80, modbus[2])
	}
	if function != readHoldingFunction {
		return nil, fmt.Errorf("%w: 0x%02x", ErrUnsupportedFunction, function)
	}

	byteCount := int(modbus[2])
	if byteCount == 0 || byteCount%2 != 0 || byteCount/2 > maxReadRegisters {
		return nil, fmt.Errorf("%w: invalid Modbus byte count %d", ErrMalformedFrame, byteCount)
	}
	if len(modbus) != 3+byteCount+2 {
		return nil, fmt.Errorf("%w: Modbus response length mismatch", ErrMalformedFrame)
	}
	if byteCount != int(expectedCount)*2 {
		return nil, ErrIdentityMismatch
	}

	registers := make([]uint16, byteCount/2)
	for i := range registers {
		registers[i] = binary.BigEndian.Uint16(modbus[3+i*2 : 5+i*2])
	}
	return registers, nil
}

func buildFrame(control uint16, serial uint32, sequence uint16, payload []byte) []byte {
	frame := make([]byte, v5HeaderSize, v5HeaderSize+len(payload)+v5TrailerSize)
	frame[0] = v5Start
	binary.LittleEndian.PutUint16(frame[1:3], uint16(len(payload)))
	binary.LittleEndian.PutUint16(frame[3:5], control)
	binary.LittleEndian.PutUint16(frame[5:7], sequence)
	binary.LittleEndian.PutUint32(frame[7:11], serial)
	frame = append(frame, payload...)
	frame = append(frame, additiveChecksum(frame[1:]), v5End)
	return frame
}

func validateModbusCRC(frame []byte) error {
	want := CRC16(frame[:len(frame)-2])
	got := binary.LittleEndian.Uint16(frame[len(frame)-2:])
	if got != want {
		return fmt.Errorf("%w: got %04x want %04x", ErrCRC, got, want)
	}
	return nil
}

func additiveChecksum(data []byte) byte {
	var checksum byte
	for _, b := range data {
		checksum += b
	}
	return checksum
}
