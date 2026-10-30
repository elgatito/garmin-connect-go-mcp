package server

import (
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
