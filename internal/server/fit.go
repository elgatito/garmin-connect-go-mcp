package server

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile"
)

type fitField struct {
	Name        string
	Value       any
	Units       string
	Num         int
	Raw         any
	BaseType    string
	ProfileType string
	ValueNames  any
	FieldType   string
}
type fitMessage struct {
	Name   string
	Fields []fitField
	Num    int
}

func extractFIT(raw []byte) ([]byte, error) {
	if bytes.HasPrefix(raw, []byte("PK")) {
		z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, err
		}
		for _, f := range z.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".fit") {
				r, err := f.Open()
				if err != nil {
					return nil, err
				}
				b, err := io.ReadAll(r)
				r.Close()
				return b, err
			}
		}
		return nil, fmt.Errorf("ZIP archive contains no .fit files")
	}
	if bytes.HasPrefix(raw, []byte{0x1f, 0x8b}) {
		r, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	return raw, nil
}
func fitValue(v any, scale, offset float64, isDate bool) any {
	if v == nil {
		return nil
	}
	r := reflect.ValueOf(v)
	if r.Kind() == reflect.Slice || r.Kind() == reflect.Array {
		out := make([]any, r.Len())
		for i := range out {
			out[i] = fitValue(r.Index(i).Interface(), scale, offset, isDate)
		}
		return out
	}
	var n float64
	switch r.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n = float64(r.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n = float64(r.Uint())
	case reflect.Float32, reflect.Float64:
		n = r.Float()
	default:
		return v
	}
	if isDate && n >= 0x10000000 {
		return time.Unix(int64(n)+631065600, 0).UTC().Format("2006-01-02T15:04:05")
	}
	if scale == 0 {
		scale = 1
	}
	if scale != 1 || offset != 0 {
		return n/scale - offset
	}
	return v
}
func decodeFIT(raw []byte) ([]fitMessage, error) {
	data, err := extractFIT(raw)
	if err != nil {
		return nil, err
	}
	decoded, err := decoder.New(bytes.NewReader(data), decoder.WithNoComponentExpansion()).Decode()
	if err != nil {
		return nil, err
	}
	rawDevelopers, err := rawDeveloperFields(data)
	if err != nil {
		return nil, err
	}
	out := make([]fitMessage, 0, len(decoded.Messages))
	devFields := map[[2]int]map[string]any{}
	for messageIndex, msg := range decoded.Messages {
		name := msg.Num.String()
		if strings.HasPrefix(name, "MesgNumInvalid(") {
			name = fmt.Sprintf("unknown_%d", msg.Num)
		}
		entry := fitMessage{Name: name, Num: int(msg.Num)}
		for _, f := range msg.Fields {
			if f.FieldBase == nil {
				continue
			}
			name := f.Name
			if name == "" || name == "unknown" {
				name = fmt.Sprintf("unknown_%d", f.Num)
			}
			value := f.Value.Any()
			if !f.Value.Valid(f.BaseType) {
				value = nil
			}
			decodedValue := fitValue(value, f.Scale, f.Offset, f.Type == profile.DateTime || f.Type == profile.LocalDateTime)
			var valueNames any
			if label, ok := fitEnumLabel(f.Type, decodedValue); ok {
				valueNames = label
				if _, sequence := decodedValue.([]any); !sequence {
					decodedValue = label
				}
			}
			entry.Fields = append(entry.Fields, fitField{Name: name, Num: int(f.Num), Value: decodedValue, Units: f.Units, Raw: value, BaseType: f.BaseType.String(), ProfileType: f.Type.String(), ValueNames: valueNames})
		}
		if entry.Name == "field_description" {
			info := map[string]any{}
			for _, field := range entry.Fields {
				info[field.Name] = field.Value
			}
			index, iok := fitNumber(info["developer_data_index"])
			number, nok := fitNumber(info["field_definition_number"])
			if iok && nok {
				devFields[[2]int{int(index), int(number)}] = info
			}
		}
		for _, f := range msg.DeveloperFields {
			info := devFields[[2]int{int(f.DeveloperDataIndex), int(f.Num)}]
			name := firstFITString(info["field_name"])
			if name == "" {
				name = fmt.Sprintf("unknown_%d", f.Num)
			}
			units := firstFITString(info["units"])
			scale, _ := fitNumber(info["scale"])
			offset, _ := fitNumber(info["offset"])
			raw := f.Value.Any()
			entry.Fields = append(entry.Fields, fitField{Name: name, Num: int(f.Num), Value: fitValue(raw, scale, offset, false), Raw: raw, Units: units, FieldType: "developer"})
		}
		if messageIndex < len(rawDevelopers) {
			for _, raw := range rawDevelopers[messageIndex] {
				found := false
				for _, existing := range msg.DeveloperFields {
					if int(existing.Num) == raw.number && int(existing.DeveloperDataIndex) == raw.index {
						found = true
						break
					}
				}
				if !found {
					entry.Fields = append(entry.Fields, fitField{Name: fmt.Sprintf("unknown_%d", raw.number), Num: raw.number, Value: raw.value, Raw: raw.value, FieldType: "developer"})
				}
			}
		}
		applyFITContextualEnums(&entry)
		out = append(out, entry)
	}
	return out, nil
}

func firstFITString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if values, ok := v.([]any); ok && len(values) > 0 {
		if s, ok := values[0].(string); ok {
			return s
		}
	}
	return ""
}

func applyFITContextualEnums(message *fitMessage) {
	selector, target := "", ""
	if message.Name == "set" {
		selector, target = "category", "category_subtype"
	}
	if message.Name == "exercise_title" {
		selector, target = "exercise_category", "exercise_name"
	}
	if selector == "" {
		return
	}
	var categories any
	for _, field := range message.Fields {
		if field.Name == selector {
			categories = field.Value
			if field.ValueNames != nil {
				categories = field.ValueNames
			}
			break
		}
	}
	selectors, ok := categories.([]any)
	if !ok {
		selectors = []any{categories}
	}
	for i := range message.Fields {
		f := &message.Fields[i]
		if f.Name != target || f.ValueNames != nil {
			continue
		}
		values, sequence := f.Value.([]any)
		if !sequence {
			values = []any{f.Value}
		}
		names := make([]any, len(values))
		found := false
		for j, value := range values {
			if j >= len(selectors) {
				break
			}
			category, ok := selectors[j].(string)
			if !ok {
				continue
			}
			kind, ok := fitExerciseType(category)
			if !ok {
				continue
			}
			if name, ok := fitEnumLabel(kind, value); ok {
				names[j] = name
				found = true
			}
		}
		if found {
			if sequence {
				f.ValueNames = names
			} else {
				f.ValueNames = names[0]
			}
		}
	}
}
