package server

import (
	"bytes"
	"encoding/hex"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/proto"
)

type rawDeveloperField struct {
	number, index int
	value         map[string]string
}
type rawFITDefinition struct {
	offset int
	fields []struct{ number, size, index int }
}

func rawDeveloperFields(data []byte) ([][]rawDeveloperField, error) {
	var definitions [16]rawFITDefinition
	records := [][]rawDeveloperField{}
	_, err := decoder.NewRaw().Decode(bytes.NewReader(data), func(flag decoder.RawFlag, b []byte) error {
		switch flag {
		case decoder.RawFlagFileHeader:
			definitions = [16]rawFITDefinition{}
		case decoder.RawFlagMesgDef:
			local := int(proto.LocalMesgNum(b[0]))
			count := int(b[5])
			def := rawFITDefinition{offset: 1}
			for i := 0; i < count; i++ {
				def.offset += int(b[6+i*3+1])
			}
			pos := 6 + count*3
			if b[0]&proto.DevDataMask != 0 {
				num := int(b[pos])
				pos++
				for i := 0; i < num; i++ {
					def.fields = append(def.fields, struct{ number, size, index int }{int(b[pos]), int(b[pos+1]), int(b[pos+2])})
					pos += 3
				}
			}
			definitions[local] = def
		case decoder.RawFlagMesgData:
			def := definitions[int(proto.LocalMesgNum(b[0]))]
			offset := def.offset
			fields := []rawDeveloperField{}
			for _, field := range def.fields {
				end := offset + field.size
				fields = append(fields, rawDeveloperField{field.number, field.index, map[string]string{"encoding": "hex", "value": hex.EncodeToString(b[offset:end])}})
				offset = end
			}
			records = append(records, fields)
		}
		return nil
	})
	return records, err
}
