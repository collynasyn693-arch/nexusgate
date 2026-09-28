package telemetry

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// BinaryFrame constants.
const (
	BinaryFrameSize    = 64
	MagicHeader        = 0x4E47544D // "NGTM" in ASCII
	CurrentVersion     = 1
	FrameTypeSnapshot  = 1
	FrameTypeHeartbeat = 2
)

var (
	ErrBufferTooShort     = errors.New("buffer smaller than 64 bytes")
	ErrCorruptMagic       = errors.New("corrupt magic header in binary frame")
	ErrUnsupportedVersion = errors.New("unsupported binary frame version")
	ErrChecksumMismatch   = errors.New("binary frame CRC32 checksum mismatch")
)

// BinaryFrame represents a compact 64-byte IPC snapshot streamed over UDS to observatory clients.
// All multi-byte numeric fields are serialized in BigEndian order.
type BinaryFrame struct {
	Version           uint16 // Frame wire format version (CurrentVersion = 1)
	FrameType         uint16 // Type of frame (1 = Snapshot, 2 = Heartbeat)
	TimestampUnixNano int64  // Snapshot nanosecond epoch
	TotalRequests     uint64 // Cumulative gateway requests served
	ActiveConns       uint32 // Current active connections
	RPS1s             uint32 // Moving 1s throughput in milli-RPS (RPS * 1000)
	P50LatencyUs      uint32 // 50th percentile latency in microseconds
	P90LatencyUs      uint32 // 90th percentile latency in microseconds
	P99LatencyUs      uint32 // 99th percentile latency in microseconds
	Status2xxCount    uint32 // 2xx responses in 60s window
	Status3xxCount    uint32 // 3xx responses in 60s window
	Status4xxCount    uint32 // 4xx responses in 60s window
	Status5xxCount    uint32 // 5xx responses in 60s window
	CRC32             uint32 // IEEE CRC32 checksum of bytes [0..59]
}

// Encode serializes the BinaryFrame into the destination buffer (minimum 64 bytes).
// Performs 0 heap allocations when dest is pre-allocated.
func (f *BinaryFrame) Encode(dest []byte) error {
	if len(dest) < BinaryFrameSize {
		return ErrBufferTooShort
	}

	binary.BigEndian.PutUint32(dest[0:4], MagicHeader)
	binary.BigEndian.PutUint16(dest[4:6], f.Version)
	binary.BigEndian.PutUint16(dest[6:8], f.FrameType)
	binary.BigEndian.PutUint64(dest[8:16], uint64(f.TimestampUnixNano))
	binary.BigEndian.PutUint64(dest[16:24], f.TotalRequests)
	binary.BigEndian.PutUint32(dest[24:28], f.ActiveConns)
	binary.BigEndian.PutUint32(dest[28:32], f.RPS1s)
	binary.BigEndian.PutUint32(dest[32:36], f.P50LatencyUs)
	binary.BigEndian.PutUint32(dest[36:40], f.P90LatencyUs)
	binary.BigEndian.PutUint32(dest[40:44], f.P99LatencyUs)
	binary.BigEndian.PutUint32(dest[44:48], f.Status2xxCount)
	binary.BigEndian.PutUint32(dest[48:52], f.Status3xxCount)
	binary.BigEndian.PutUint32(dest[52:56], f.Status4xxCount)
	binary.BigEndian.PutUint32(dest[56:60], f.Status5xxCount)

	checksum := crc32.ChecksumIEEE(dest[0:60])
	f.CRC32 = checksum
	binary.BigEndian.PutUint32(dest[60:64], checksum)
	return nil
}

// Decode deserializes a 64-byte binary payload into the BinaryFrame struct,
// validating the magic header, protocol version, and CRC32 checksum.
func (f *BinaryFrame) Decode(src []byte) error {
	if len(src) < BinaryFrameSize {
		return ErrBufferTooShort
	}

	magic := binary.BigEndian.Uint32(src[0:4])
	if magic != MagicHeader {
		return fmt.Errorf("%w: expected 0x%08X, got 0x%08X", ErrCorruptMagic, MagicHeader, magic)
	}

	version := binary.BigEndian.Uint16(src[4:6])
	if version != CurrentVersion {
		return fmt.Errorf("%w: supported %d, got %d", ErrUnsupportedVersion, CurrentVersion, version)
	}

	expectedCRC := binary.BigEndian.Uint32(src[60:64])
	actualCRC := crc32.ChecksumIEEE(src[0:60])
	if expectedCRC != actualCRC {
		return fmt.Errorf("%w: expected 0x%08X, got 0x%08X", ErrChecksumMismatch, expectedCRC, actualCRC)
	}

	f.Version = version
	f.FrameType = binary.BigEndian.Uint16(src[6:8])
	f.TimestampUnixNano = int64(binary.BigEndian.Uint64(src[8:16]))
	f.TotalRequests = binary.BigEndian.Uint64(src[16:24])
	f.ActiveConns = binary.BigEndian.Uint32(src[24:28])
	f.RPS1s = binary.BigEndian.Uint32(src[28:32])
	f.P50LatencyUs = binary.BigEndian.Uint32(src[32:36])
	f.P90LatencyUs = binary.BigEndian.Uint32(src[36:40])
	f.P99LatencyUs = binary.BigEndian.Uint32(src[40:44])
	f.Status2xxCount = binary.BigEndian.Uint32(src[44:48])
	f.Status3xxCount = binary.BigEndian.Uint32(src[48:52])
	f.Status4xxCount = binary.BigEndian.Uint32(src[52:56])
	f.Status5xxCount = binary.BigEndian.Uint32(src[56:60])
	f.CRC32 = expectedCRC

	return nil
}

// ReadFrame reads exactly one 64-byte frame from the reader using io.ReadFull,
// ensuring clean stream frame handling over Unix Domain Sockets without partial read corruption.
func ReadFrame(r io.Reader, frame *BinaryFrame, buf []byte) error {
	if len(buf) < BinaryFrameSize {
		buf = make([]byte, BinaryFrameSize)
	}
	if _, err := io.ReadFull(r, buf[:BinaryFrameSize]); err != nil {
		return err
	}
	return frame.Decode(buf[:BinaryFrameSize])
}
