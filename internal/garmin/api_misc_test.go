package garmin

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestEncodeWeightFITHeaderAndCRC(t *testing.T) {
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.Local)
	data := encodeWeightFIT(date, BodyComposition{
		Weight:            70.0,
		PercentFat:        fitNumber(15.0),
		VisceralFatMass:   fitNumber(0.5),
		PhysiqueRating:    intPointer(3),
		VisceralFatRating: intPointer(4),
	})

	if len(data) < 14 {
		t.Fatalf("FIT file too short: %d bytes", len(data))
	}
	if data[0] != fitHeaderSize || data[1] != fitProtocol {
		t.Fatalf("header = %v", data[:2])
	}
	if string(data[8:12]) != ".FIT" {
		t.Fatalf("data type = %q", data[8:12])
	}
	declared := binary.LittleEndian.Uint32(data[4:8])
	if int(declared) != len(data)-fitHeaderSize-2 {
		t.Fatalf("data size = %d, want %d", declared, len(data)-fitHeaderSize-2)
	}
	crc := uint16(0)
	for _, b := range data[:len(data)-2] {
		crc = fitCRC(crc, b)
	}
	if got := binary.LittleEndian.Uint16(data[len(data)-2:]); got != crc {
		t.Fatalf("crc = %#x, want %#x", got, crc)
	}
	// Definition messages for file_id (0), file_creator (49), device_info (23)
	// and weight_scale (30) must all be present.
	for _, msg := range []byte{0x40, 0x41, 0x42, 0x43} {
		if !bytes.Contains(data, []byte{msg}) {
			t.Fatalf("missing definition message %#x", msg)
		}
	}
	// Definition messages carry real FIT field numbers, not positions:
	// weight_scale (msg 30, 13 fields) starts with field 253 (timestamp).
	if !bytes.Contains(data, []byte{0x43, 0, 0, 30, 0, 13, 253, 4, 0x86}) {
		t.Fatal("weight_scale definition has wrong field numbers")
	}
	// file_id (msg 0, 6 fields) starts with field 3 (serial, uint32z).
	if !bytes.Contains(data, []byte{0x40, 0, 0, 0, 0, 6, 3, 4, 0x8C}) {
		t.Fatal("file_id definition has wrong field numbers")
	}
}

func TestDeviceIDStringAllowsNumericKeysAndRejectsEmpty(t *testing.T) {
	if got, err := deviceIDString(123456789); err != nil || got != "123456789" {
		t.Fatalf("int = %q, %v", got, err)
	}
	if got, err := deviceIDString("abc123"); err != nil || got != "abc123" {
		t.Fatalf("string = %q, %v", got, err)
	}
	if _, err := deviceIDString(""); err == nil {
		t.Fatal("empty device id accepted")
	}
	if _, err := deviceIDString(0); err == nil {
		t.Fatal("zero device id accepted")
	}
}

func intPointer(v int) *int { return &v }
