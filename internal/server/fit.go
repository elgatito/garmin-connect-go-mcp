package server

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

// fitField is one decoded FIT field in fitparse-compatible form.
type fitField struct {
	Name             string
	Value            any
	Units            string
	DefinitionNumber *int
	BaseType         string
	ProfileType      string
	FieldType        string
	ValueName        *string
	ValueNames       []any
	RawValue         any
	HasRawValue      bool
}

// fitMessage is one decoded FIT message in file order.
type fitMessage struct {
	Name                string
	Fields              []fitField
	GlobalMessageNumber int
}

const fitEpochOffsetSeconds = 631065600

// extractFITBytes ports _extract_fit_bytes: Garmin's ORIGINAL download is a
// ZIP holding the .fit, with gzip and raw FIT as fallbacks.
func extractFITBytes(raw []byte) ([]byte, error) {
	if len(raw) >= 2 && raw[0] == 'P' && raw[1] == 'K' {
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, err
		}
		for _, file := range zr.File {
			if !strings.HasSuffix(strings.ToLower(file.Name), ".fit") {
				continue
			}
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
		return nil, errors.New("ZIP archive contains no .fit files")
	}
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	}
	return raw, nil
}

// decodeFIT decodes a FIT file, extracting it from Garmin's ZIP wrapper first.
func decodeFIT(raw []byte) ([]fitMessage, error) {
	fitBytes, err := extractFITBytes(raw)
	if err != nil {
		return nil, err
	}
	file, err := decoder.New(bytes.NewReader(fitBytes)).Decode()
	if err != nil {
		return nil, err
	}
	out := make([]fitMessage, 0, len(file.Messages))
	for i := range file.Messages {
		message := &file.Messages[i]
		decoded := fitMessage{
			Name:                fitMessageName(message.Num),
			GlobalMessageNumber: int(uint16(message.Num)),
		}
		for j := range message.Fields {
			decoded.Fields = append(decoded.Fields, decodeFitField(&message.Fields[j], message))
		}
		for j := range message.DeveloperFields {
			dev := &message.DeveloperFields[j]
			decoded.Fields = append(decoded.Fields, fitField{
				Name:             fmt.Sprintf("unknown_dev_%d_%d", dev.DeveloperDataIndex, dev.Num),
				Value:            jsonSafeFitValue(dev.Value.Any()),
				FieldType:        "devfield",
				RawValue:         jsonSafeFitValue(dev.Value.Any()),
				HasRawValue:      true,
				DefinitionNumber: fitIntPtr(int(dev.Num)),
			})
		}
		out = append(out, decoded)
	}
	return out, nil
}

func fitMessageName(num typedef.MesgNum) string {
	name := num.String()
	if strings.HasPrefix(name, "MesgNumInvalid(") {
		return fmt.Sprintf("unknown_%d", uint16(num))
	}
	return strings.ToLower(name)
}

func fitIntPtr(v int) *int { return &v }

func decodeFitField(field *proto.Field, message *proto.Message) fitField {
	name := field.Name
	fieldType := field.Type
	scale, offset := field.Scale, field.Offset
	units := field.Units
	fieldKind := "field"
	if sub := field.SubFieldSubstitution(message); sub != nil {
		name = sub.Name
		fieldType = sub.Type
		scale = sub.Scale
		offset = sub.Offset
		units = sub.Units
		fieldKind = "subfield"
	} else if field.IsExpandedField {
		fieldKind = "component"
	}
	if name == "" || name == "unknown" {
		name = fmt.Sprintf("unknown_%d", field.Num)
	}

	raw := field.Value.Any()
	rawCopy := copyFitValue(raw)
	value := rawCopy
	var valueName *string
	var valueNames []any

	dateTime := fieldType == profile.DateTime || fieldType == profile.LocalDateTime
	switch {
	case dateTime:
		if seconds, ok := fitUint64(raw); ok {
			value = fitTimeISO(seconds)
		}
	case !isFitSlice(raw):
		if label, ok := fitEnumLabel(fieldType, raw); ok {
			value = label
			valueName = &label
		} else {
			value = applyFitScaleOffset(value, scale, offset)
		}
	default:
		if labels, ok := fitEnumLabels(fieldType, raw); ok {
			valueNames = labels
		} else {
			value = applyFitScaleOffset(value, scale, offset)
		}
	}

	decoded := fitField{
		Name:             name,
		Value:            value,
		Units:            units,
		DefinitionNumber: fitIntPtr(int(field.Num)),
		BaseType:         field.BaseType.String(),
		ProfileType:      fieldType.String(),
		FieldType:        fieldKind,
		ValueName:        valueName,
		ValueNames:       valueNames,
		RawValue:         rawCopy,
		HasRawValue:      true,
	}
	return decoded
}

func copyFitValue(value any) any {
	switch v := value.(type) {
	case []uint8:
		return append([]uint8(nil), v...)
	case []int8:
		return append([]int8(nil), v...)
	case []uint16:
		return append([]uint16(nil), v...)
	case []int16:
		return append([]int16(nil), v...)
	case []uint32:
		return append([]uint32(nil), v...)
	case []int32:
		return append([]int32(nil), v...)
	case []uint64:
		return append([]uint64(nil), v...)
	case []int64:
		return append([]int64(nil), v...)
	case []float32:
		return append([]float32(nil), v...)
	case []float64:
		return append([]float64(nil), v...)
	case []bool:
		return append([]bool(nil), v...)
	case []string:
		return append([]string(nil), v...)
	}
	return value
}

func isFitSlice(value any) bool {
	switch value.(type) {
	case []uint8, []int8, []uint16, []int16, []uint32, []int32,
		[]uint64, []int64, []float32, []float64, []bool, []string, []any:
		return true
	}
	return false
}

func fitSliceToAny(value any) []any {
	var out []any
	switch v := value.(type) {
	case []any:
		return v
	case []uint8:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []int8:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []uint16:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []int16:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []uint32:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []int32:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []uint64:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []int64:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []float32:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []float64:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []bool:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	case []string:
		out = make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
	}
	return out
}

func applyFitScaleOffset(value any, scale, offset float64) any {
	if scale == 0 {
		scale = 1
	}
	if scale == 1 && offset == 0 {
		return value
	}
	if isFitSlice(value) {
		items := fitSliceToAny(value)
		out := make([]any, len(items))
		for i, item := range items {
			if f, ok := fitNumber(item); ok {
				out[i] = f/scale - offset
			} else {
				out[i] = nil
			}
		}
		return out
	}
	if f, ok := fitNumber(value); ok {
		return f/scale - offset
	}
	return value
}

func fitTimeISO(seconds uint64) string {
	return time.Unix(int64(seconds)+fitEpochOffsetSeconds, 0).UTC().Format("2006-01-02T15:04:05")
}

func fitUint64(value any) (uint64, bool) {
	switch v := value.(type) {
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	case int8:
		return uint64(v), true
	case int16:
		return uint64(v), true
	case int32:
		return uint64(v), true
	case int64:
		return uint64(v), true
	case int:
		return uint64(v), true
	case float64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	}
	return 0, false
}

// fitEnumLabel maps a scalar profile enum to fitparse's decoded label.
func fitEnumLabel(fieldType profile.ProfileType, raw any) (string, bool) {
	n, ok := fitUint64(raw)
	if !ok {
		return "", false
	}
	var label string
	switch fieldType {
	case profile.Sport:
		label = typedef.Sport(n).String()
	case profile.SubSport:
		label = typedef.SubSport(n).String()
	case profile.Event:
		label = typedef.Event(n).String()
	case profile.EventType:
		label = typedef.EventType(n).String()
	case profile.Manufacturer:
		label = typedef.Manufacturer(n).String()
	case profile.Activity:
		label = typedef.Activity(n).String()
	case profile.ActivityType:
		label = typedef.ActivityType(n).String()
	case profile.File:
		label = typedef.File(n).String()
	case profile.Gender:
		label = typedef.Gender(n).String()
	case profile.SetType:
		label = typedef.SetType(n).String()
	case profile.ExerciseCategory:
		label = typedef.ExerciseCategory(n).String()
	case profile.Intensity:
		label = typedef.Intensity(n).String()
	case profile.SessionTrigger:
		label = typedef.SessionTrigger(n).String()
	case profile.LapTrigger:
		label = typedef.LapTrigger(n).String()
	case profile.SwimStroke:
		label = typedef.SwimStroke(n).String()
	case profile.BatteryStatus:
		label = typedef.BatteryStatus(n).String()
	case profile.BodyLocation:
		label = typedef.BodyLocation(n).String()
	case profile.SourceType:
		label = typedef.SourceType(n).String()
	case profile.Weight:
		label = typedef.Weight(n).String()
	default:
		return "", false
	}
	if strings.Contains(label, "Invalid(") {
		return "", false
	}
	return label, true
}

func fitEnumLabels(fieldType profile.ProfileType, raw any) ([]any, bool) {
	items := fitSliceToAny(raw)
	out := make([]any, len(items))
	anyLabel := false
	for i, item := range items {
		if label, ok := fitEnumLabel(fieldType, item); ok {
			out[i] = label
			anyLabel = true
		}
	}
	if !anyLabel {
		return nil, false
	}
	return out, true
}

func fitNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

// jsonSafeFitValue converts a decoded FIT value to a JSON-safe representation.
func jsonSafeFitValue(value any) any {
	switch v := value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64:
		return v
	case []byte:
		return map[string]any{"encoding": "hex", "value": fmt.Sprintf("%x", v)}
	case time.Time:
		return v.Format("2006-01-02T15:04:05")
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = jsonSafeFitValue(item)
		}
		return out
	}
	if isFitSlice(value) {
		items := fitSliceToAny(value)
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = jsonSafeFitValue(item)
		}
		return out
	}
	if iso, ok := value.(interface{ ISOFormat() string }); ok {
		return iso.ISOFormat()
	}
	return fmt.Sprint(value)
}

// fitFieldMap renders one field in the shape _serialize_fit_field produces.
func fitFieldMap(field fitField) map[string]any {
	out := map[string]any{
		"name":  field.Name,
		"value": jsonSafeFitValue(field.Value),
	}
	if field.Units != "" {
		out["units"] = field.Units
	}
	if field.DefinitionNumber != nil {
		out["definition_number"] = *field.DefinitionNumber
	}
	if field.BaseType != "" {
		out["base_type"] = field.BaseType
	}
	if field.ProfileType != "" {
		out["profile_type"] = field.ProfileType
	}
	if field.FieldType != "" {
		out["field_type"] = field.FieldType
	}
	if len(field.ValueNames) > 0 {
		names := make([]any, len(field.ValueNames))
		for i, name := range field.ValueNames {
			names[i] = jsonSafeFitValue(name)
		}
		out["value_names"] = names
	} else if field.ValueName != nil {
		out["value_name"] = *field.ValueName
	}
	if field.HasRawValue {
		out["raw_value"] = jsonSafeFitValue(field.RawValue)
	}
	return out
}
