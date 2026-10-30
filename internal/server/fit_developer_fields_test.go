package server

import (
	"encoding/binary"
	"github.com/muktihari/fit/kit/hash/crc16"
	"os"
	"testing"
)

func TestFITDeveloperFields(t *testing.T) {
	b, e := os.ReadFile("testdata/ActivityDevFields.fit")
	if e != nil {
		t.Fatal(e)
	}
	messages, e := decodeFIT(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, message := range messages {
		if message.Name != "record" {
			continue
		}
		for _, field := range message.Fields {
			if field.FieldType == "developer" && field.Name == "Heart Rate" && field.Units == "bpm" && field.Value != nil {
				return
			}
		}
	}
	t.Fatal("developer field name, units, or value was lost")
}

func TestFITUnknownDeveloperFieldRetainsRawBytes(t *testing.T) {
	data := []byte{12, 16, 0, 0, 13, 0, 0, 0, '.', 'F', 'I', 'T', 0x60, 0, 0, 20, 0, 0, 1, 3, 2, 0, 0, 0x7b, 0x7c, 0, 0}
	rows, err := rawDeveloperFields(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0]) != 1 || rows[0][0].number != 3 || rows[0][0].value["value"] != "7b7c" {
		t.Fatal(rows)
	}
	checksum := crc16.New()
	checksum.Write(data[:len(data)-2])
	binary.LittleEndian.PutUint16(data[len(data)-2:], checksum.Sum16())
	messages, err := decodeFIT(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || len(messages[0].Fields) != 1 || messages[0].Fields[0].Name != "unknown_3" || messages[0].Fields[0].Value.(map[string]string)["value"] != "7b7c" {
		t.Fatal(messages)
	}
}
