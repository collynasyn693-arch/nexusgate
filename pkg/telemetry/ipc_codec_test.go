package telemetry

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestBinaryFrame_RoundTrip(t *testing.T) {
	orig := BinaryFrame{
		Version:           CurrentVersion,
		FrameType:         FrameTypeSnapshot,
		TimestampUnixNano: 1700000000123456789,
		TotalRequests:     9876543210,
		ActiveConns:       42,
		RPS1s:             125500, // 125.5 RPS
		P50LatencyUs:      450,
		P90LatencyUs:      1200,
		P99LatencyUs:      8500,
		Status2xxCount:    9500,
		Status3xxCount:    200,
		Status4xxCount:    150,
		Status5xxCount:    26,
	}

	var buf [BinaryFrameSize]byte
	if err := orig.Encode(buf[:]); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var decoded BinaryFrame
	if err := decoded.Decode(buf[:]); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded != orig {
		t.Fatalf("round-trip mismatch:\norig:    %+v\ndecoded: %+v", orig, decoded)
	}
}

func TestBinaryFrame_ZeroAllocations(t *testing.T) {
	frame := BinaryFrame{
		Version:           CurrentVersion,
		FrameType:         FrameTypeSnapshot,
		TimestampUnixNano: 1700000000000000000,
		TotalRequests:     1000,
	}
	var buf [BinaryFrameSize]byte

	allocs := testing.AllocsPerRun(1000, func() {
		_ = frame.Encode(buf[:])
		_ = frame.Decode(buf[:])
	})

	if allocs != 0.0 {
		t.Fatalf("expected 0 allocs per Encode/Decode cycle, got %.2f", allocs)
	}
}

func TestBinaryFrame_EndiannessAndWireLayout(t *testing.T) {
	frame := BinaryFrame{
		Version:           1,
		FrameType:         1,
		TimestampUnixNano: 0x0102030405060708,
		TotalRequests:     0x1122334455667788,
		ActiveConns:       0x0A0B0C0D,
	}

	var buf [BinaryFrameSize]byte
	if err := frame.Encode(buf[:]); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Verify Magic bytes "NGTM" = 0x4E 0x47 0x54 0x4D (Magic bytes 0x4E, 0x47 at bytes 0..1)
	expectedMagic := []byte{'N', 'G', 'T', 'M'}
	if !bytes.Equal(buf[0:4], expectedMagic) {
		t.Errorf("magic header mismatch: got %v, want %v", buf[0:4], expectedMagic)
	}

	// Version LittleEndian 1 = 0x01 0x00
	if buf[4] != 0x01 || buf[5] != 0x00 {
		t.Errorf("version LittleEndian mismatch: got %x %x", buf[4], buf[5])
	}

	// TimestampUnixNano LittleEndian = 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01
	expectedTS := []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}
	if !bytes.Equal(buf[8:16], expectedTS) {
		t.Errorf("timestamp LittleEndian mismatch: got %x, want %x", buf[8:16], expectedTS)
	}

	// TotalRequests LittleEndian = 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11
	expectedReqs := []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}
	if !bytes.Equal(buf[16:24], expectedReqs) {
		t.Errorf("total requests LittleEndian mismatch: got %x, want %x", buf[16:24], expectedReqs)
	}

	// ActiveConns LittleEndian = 0x0D, 0x0C, 0x0B, 0x0A
	expectedConns := []byte{0x0D, 0x0C, 0x0B, 0x0A}
	if !bytes.Equal(buf[24:28], expectedConns) {
		t.Errorf("active conns LittleEndian mismatch: got %x, want %x", buf[24:28], expectedConns)
	}
}

func TestBinaryFrame_CorruptMagic(t *testing.T) {
	var buf [BinaryFrameSize]byte
	frame := BinaryFrame{Version: 1}
	_ = frame.Encode(buf[:])

	// Corrupt magic
	buf[0] = 'X'
	var decoded BinaryFrame
	err := decoded.Decode(buf[:])
	if !errors.Is(err, ErrCorruptMagic) {
		t.Fatalf("expected ErrCorruptMagic, got: %v", err)
	}
}

func TestBinaryFrame_UnsupportedVersion(t *testing.T) {
	var buf [BinaryFrameSize]byte
	frame := BinaryFrame{Version: 1}
	_ = frame.Encode(buf[:])

	// Change version to 99 in LittleEndian
	binary.LittleEndian.PutUint16(buf[4:6], 99)
	var decoded BinaryFrame
	err := decoded.Decode(buf[:])
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("expected ErrUnsupportedVersion, got: %v", err)
	}
}

func TestBinaryFrame_ChecksumMismatch(t *testing.T) {
	var buf [BinaryFrameSize]byte
	frame := BinaryFrame{Version: 1, ActiveConns: 5}
	_ = frame.Encode(buf[:])

	// Tamper with payload byte
	buf[25] ^= 0xFF

	var decoded BinaryFrame
	err := decoded.Decode(buf[:])
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got: %v", err)
	}
}

func TestBinaryFrame_BufferTooShort(t *testing.T) {
	shortBuf := make([]byte, 63)
	var frame BinaryFrame
	if err := frame.Encode(shortBuf); !errors.Is(err, ErrBufferTooShort) {
		t.Errorf("expected ErrBufferTooShort on Encode, got: %v", err)
	}
	if err := frame.Decode(shortBuf); !errors.Is(err, ErrBufferTooShort) {
		t.Errorf("expected ErrBufferTooShort on Decode, got: %v", err)
	}
}

func TestReadFrame_Stream(t *testing.T) {
	frame := BinaryFrame{
		Version:       CurrentVersion,
		FrameType:     FrameTypeSnapshot,
		TotalRequests: 555,
	}
	var buf [BinaryFrameSize]byte
	if err := frame.Encode(buf[:]); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	reader := bytes.NewReader(buf[:])
	var decoded BinaryFrame
	readBuf := make([]byte, BinaryFrameSize)
	if err := ReadFrame(reader, &decoded, readBuf); err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}
	if decoded.TotalRequests != 555 {
		t.Fatalf("expected TotalRequests 555, got %d", decoded.TotalRequests)
	}
}
