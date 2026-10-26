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

	"github.com/muktihari/fit/cmd/fitprint/printer"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile"
	"github.com/muktihari/fit/profile/basetype"
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
			decoded.Fields = append(decoded.Fields, decodeDeveloperFitField(&message.DeveloperFields[j]))
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
	rawValue, value, isArray, valid := normalizeFitValue(raw, field.BaseType)
	var valueName *string
	var valueNames []any

	if valid {
		dateTime := fieldType == profile.DateTime || fieldType == profile.LocalDateTime
		switch {
		case dateTime:
			if isArray {
				items := fitSliceToAny(value)
				converted := make([]any, len(items))
				for i, item := range items {
					if item == nil {
						continue
					}
					seconds, ok := fitUint64(item)
					if !ok {
						converted[i] = item
						continue
					}
					converted[i] = fitTimeISO(seconds)
				}
				value = converted
			} else if seconds, ok := fitUint64(value); ok {
				value = fitTimeISO(seconds)
			}
		case isArray:
			if labels, ok := fitEnumLabels(fieldType, value); ok {
				valueNames = labels
			} else {
				value = applyFitScaleOffset(value, scale, offset)
			}
			if len(valueNames) == 0 {
				if names, list, ok := contextualFitValueNames(field, message, name); ok {
					valueNames = names
					if !list {
						valueName = fitStringPtr(fmt.Sprint(names[0]))
						valueNames = nil
					}
				}
			}
		default:
			if label, ok := fitEnumLabel(fieldType, value); ok {
				value = label
				valueName = &label
			} else if names, _, ok := contextualFitValueNames(field, message, name); ok {
				if len(names) > 0 && names[0] != nil {
					valueName = fitStringPtr(fmt.Sprint(names[0]))
				}
			} else {
				value = applyFitScaleOffset(value, scale, offset)
			}
		}
	}

	return fitField{
		Name:             name,
		Value:            value,
		Units:            units,
		DefinitionNumber: fitIntPtr(int(field.Num)),
		BaseType:         field.BaseType.String(),
		ProfileType:      fieldType.String(),
		FieldType:        fieldKind,
		ValueName:        valueName,
		ValueNames:       valueNames,
		RawValue:         rawValue,
		HasRawValue:      true,
	}
}

// normalizeFitValue applies fitparse's invalid-base-type rule before any
// scaling or ISO conversion. Invalid values become nil (JSON null), including
// each invalid element of an array. Single-element non-byte arrays collapse to
// a scalar exactly as fitparse flattens them.
func normalizeFitValue(raw any, baseType basetype.BaseType) (rawValue, value any, isArray, valid bool) {
	if !isFitSlice(raw) {
		valid = proto.Any(raw).Valid(baseType)
		if valid {
			return copyFitValue(raw), raw, false, true
		}
		return nil, nil, false, false
	}

	if baseType == basetype.Byte {
		items, _ := raw.([]byte)
		for _, item := range items {
			if item != 0xFF {
				return copyFitValue(raw), copyFitValue(raw), true, true
			}
		}
		return nil, nil, true, false
	}

	items, anyValid := normalizeFitArray(raw, baseType)
	if !anyValid {
		return nil, nil, true, false
	}
	if len(items) == 1 {
		if items[0] == nil {
			return nil, nil, false, false
		}
		return items[0], items[0], false, true
	}
	return items, items, true, true
}

func normalizeFitArray(raw any, baseType basetype.BaseType) ([]any, bool) {
	items := fitSliceToAny(raw)
	anyValid := false
	for i, item := range items {
		if !proto.Any(item).Valid(baseType) {
			items[i] = nil
			continue
		}
		anyValid = true
	}
	if !anyValid {
		return nil, false
	}
	return items, true
}

func decodeDeveloperFitField(dev *proto.DeveloperField) fitField {
	baseType := fitValueBaseType(dev.Value)
	rawValue, value, _, _ := normalizeFitValue(dev.Value.Any(), baseType)
	return fitField{
		Name:             fmt.Sprintf("unknown_dev_%d_%d", dev.DeveloperDataIndex, dev.Num),
		Value:            value,
		FieldType:        "devfield",
		RawValue:         rawValue,
		HasRawValue:      true,
		DefinitionNumber: fitIntPtr(int(dev.Num)),
	}
}

func fitValueBaseType(value proto.Value) basetype.BaseType {
	switch value.Type() {
	case proto.TypeBool, proto.TypeSliceBool:
		return basetype.Enum
	case proto.TypeInt8, proto.TypeSliceInt8:
		return basetype.Sint8
	case proto.TypeUint8, proto.TypeSliceUint8:
		return basetype.Uint8
	case proto.TypeInt16, proto.TypeSliceInt16:
		return basetype.Sint16
	case proto.TypeUint16, proto.TypeSliceUint16:
		return basetype.Uint16
	case proto.TypeInt32, proto.TypeSliceInt32:
		return basetype.Sint32
	case proto.TypeUint32, proto.TypeSliceUint32:
		return basetype.Uint32
	case proto.TypeInt64, proto.TypeSliceInt64:
		return basetype.Sint64
	case proto.TypeUint64, proto.TypeSliceUint64:
		return basetype.Uint64
	case proto.TypeFloat32, proto.TypeSliceFloat32:
		return basetype.Float32
	case proto.TypeFloat64, proto.TypeSliceFloat64:
		return basetype.Float64
	case proto.TypeString, proto.TypeSliceString:
		return basetype.String
	}
	return basetype.Uint8
}

func fitStringPtr(value string) *string { return &value }

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

// fitEnumLabel resolves a profile enum through the fit module's generated
// typedef table, matching fitparse's decoded labels for every profile type.
func fitEnumLabel(fieldType profile.ProfileType, raw any) (string, bool) {
	n, ok := fitUint64(raw)
	if !ok {
		return "", false
	}
	value, ok := fitProtoValue(fieldType, n)
	if !ok {
		return "", false
	}
	label := printer.TypedefString(fieldType, value)
	if label == "" || strings.Contains(label, "Invalid(") {
		return "", false
	}
	return label, true
}

func fitProtoValue(fieldType profile.ProfileType, n uint64) (proto.Value, bool) {
	switch fieldType.BaseType() {
	case basetype.Enum, basetype.Byte, basetype.Uint8, basetype.Uint8z:
		return proto.Uint8(uint8(n)), true
	case basetype.Sint8:
		return proto.Int8(int8(n)), true
	case basetype.Uint16, basetype.Uint16z:
		return proto.Uint16(uint16(n)), true
	case basetype.Sint16:
		return proto.Int16(int16(n)), true
	case basetype.Uint32, basetype.Uint32z:
		return proto.Uint32(uint32(n)), true
	case basetype.Sint32:
		return proto.Int32(int32(n)), true
	case basetype.Uint64, basetype.Uint64z:
		return proto.Uint64(n), true
	case basetype.Sint64:
		return proto.Int64(int64(n)), true
	case basetype.Float32:
		return proto.Float32(float32(n)), true
	case basetype.Float64:
		return proto.Float64(float64(n)), true
	}
	return proto.Value{}, false
}

// contextualFitValueNames ports _contextual_fit_value_names for the two FIT
// dynamic fields whose enum type depends on a sibling field.
func contextualFitValueNames(field *proto.Field, message *proto.Message, fieldName string) ([]any, bool, bool) {
	var selectorName string
	switch {
	case message.Num == typedef.MesgNumSet && fieldName == "category_subtype":
		selectorName = "category"
	case message.Num == typedef.MesgNumExerciseTitle && fieldName == "exercise_name":
		selectorName = "exercise_category"
	default:
		return nil, false, false
	}
	selector := findProtoField(message, selectorName)
	if selector == nil {
		return nil, false, false
	}

	selectorValue := selector.Value.Any()
	subtypeValue := field.Value.Any()
	selectorIsArray := isFitSlice(selectorValue)
	subtypeIsArray := isFitSlice(subtypeValue)
	selectorItems := fitSliceToAny(selectorValue)
	var subtypeItems []any
	if subtypeIsArray {
		subtypeItems = fitSliceToAny(subtypeValue)
	} else {
		subtypeItems = []any{subtypeValue}
	}

	names := make([]any, len(subtypeItems))
	anyName := false
	for i, subtype := range subtypeItems {
		selectorRaw := selectorValue
		if selectorIsArray {
			if i >= len(selectorItems) {
				continue
			}
			selectorRaw = selectorItems[i]
		}
		selectorLabel, ok := fitEnumLabel(profile.ExerciseCategory, selectorRaw)
		if !ok {
			continue
		}
		exerciseType := profile.ProfileTypeFromString(selectorLabel + "_exercise_name")
		if exerciseType == profile.Invalid {
			continue
		}
		if label, ok := fitEnumLabel(exerciseType, subtype); ok {
			names[i] = label
			anyName = true
		}
	}
	if !anyName {
		return nil, false, false
	}
	if !subtypeIsArray {
		return names[:1], false, true
	}
	return names, true, true
}

func findProtoField(message *proto.Message, name string) *proto.Field {
	for i := range message.Fields {
		if message.Fields[i].Name == name {
			return &message.Fields[i]
		}
	}
	return nil
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
