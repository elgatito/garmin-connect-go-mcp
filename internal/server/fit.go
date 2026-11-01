package server

// FIT decoding shaped like fitparse 1.2.0 (the Python server's decoder), on top of github.com/muktihari/fit.
// The library decodes the binary records (no component expansion); this file re-applies fitparse's rules:
// field order, sub-field resolution, one-level component expansion, invalid values as nil, enum labels,
// scale/offset and the date_time/local_date_time/localtime_into_day/bool processors.

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/muktihari/fit/cmd/fitprint/printer"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

// fitMessage is one FIT data message (fitparse DataMessage).
type fitMessage struct {
	Name   string // profile snake_case name, or unknown_<num>
	Num    int    // global message number
	Fields []fitField
}

// fitField is one fitparse FieldData.
type fitField struct {
	Name        string // profile name, sub-field name, developer field name, or unknown_<num>
	Value       any    // nil (invalid), bool, int64, uint64, float64, string, time.Time, or []any of those
	Units       string // "" when there are none
	Num         int    // field definition number
	BaseType    string // FIT base type name, e.g. "uint16"
	ProfileType string // profile type name, e.g. "date_time", "sport", or the base type name
	Kind        string // fitparse field_type: "field", "subfield" or "devfield"
	Parent      string // the main field's name when this is a resolved sub-field
	Raw         any    // the value before labels, scale/offset and processors
	pt          profile.ProfileType
}

// fitUTCReference is fitparse's UTC_REFERENCE (1989-12-31T00:00:00Z).
const fitUTCReference = 631065600

// extractFitBytes is _extract_fit_bytes: Garmin's original download is a ZIP holding the .fit; gzip and raw FIT pass too.
func extractFitBytes(raw []byte) ([]byte, error) {
	if bytes.HasPrefix(raw, []byte("PK")) {
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".fit") {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errors.New("ZIP archive contains no .fit files")
	}
	if bytes.HasPrefix(raw, []byte{0x1f, 0x8b}) {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(zr)
	}
	return raw, nil
}

// fitMessageName is fitparse's message name: the profile name, or unknown_<num> (manufacturer range included).
func fitMessageName(num typedef.MesgNum) string {
	name := num.String()
	if num >= typedef.MesgNumMfgRangeMin || strings.Contains(name, "(") {
		return fmt.Sprintf("unknown_%d", num)
	}
	return name
}

type devFieldKey struct{ index, num byte }

type devFieldDesc struct {
	name, units string
	baseType    basetype.BaseType
}

// fitDecoder receives the library's definitions and messages in file order.
type fitDecoder struct {
	msgs  []fitMessage
	accum map[typedef.MesgNum]map[byte]int64 // fitparse _accumulators
	dev   map[devFieldKey]devFieldDesc
}

// decodeFIT decodes FIT bytes (all chained FIT sequences) into fitparse-shaped messages.
func decodeFIT(raw []byte) ([]fitMessage, error) {
	d := &fitDecoder{dev: map[devFieldKey]devFieldDesc{}}
	dec := decoder.New(bytes.NewReader(raw), decoder.WithNoComponentExpansion(), decoder.WithBroadcastOnly(),
		decoder.WithMesgDefListener(d), decoder.WithMesgListener(d))
	for dec.Next() {
		d.accum = map[typedef.MesgNum]map[byte]int64{} // fitparse resets them with each file header
		if _, err := dec.Decode(); err != nil {
			return nil, err
		}
	}
	return d.msgs, nil
}

// OnMesgDef zeroes the accumulators of the components the definition's main fields carry, as fitparse does.
func (d *fitDecoder) OnMesgDef(def proto.MessageDefinition) {
	for _, fd := range def.FieldDefinitions {
		for _, c := range factory.CreateField(def.MesgNum, fd.Num).Components {
			if c.Accumulate {
				if d.accum[def.MesgNum] == nil {
					d.accum[def.MesgNum] = map[byte]int64{}
				}
				d.accum[def.MesgNum][c.FieldNum] = 0
			}
		}
	}
}

func (d *fitDecoder) OnMesg(m proto.Message) {
	fields := m.Fields
	var ts *proto.Field
	if m.Header&proto.MesgCompressedHeaderMask != 0 && len(fields) > 0 {
		ts, fields = &fields[0], fields[1:] // the library puts the compressed timestamp first, fitparse last
	}
	var out []fitField
	for i := range fields {
		out = d.appendField(out, &m, &fields[i])
	}
	for _, df := range m.DeveloperFields {
		desc, ok := d.dev[devFieldKey{df.DeveloperDataIndex, df.Num}]
		if !ok {
			continue
		}
		raw := fitRaw(df.Value, desc.baseType)
		out = append(out, fitField{Name: desc.name, Value: raw, Units: desc.units, Num: int(df.Num),
			BaseType: desc.baseType.String(), ProfileType: desc.baseType.String(), Kind: "devfield", Raw: raw,
			pt: profile.ProfileTypeFromBaseType(desc.baseType)})
	}
	if ts != nil {
		raw := fitRaw(ts.Value, basetype.Uint32)
		out = append(out, fitField{Name: "timestamp", Value: raw, Units: "s", Num: proto.FieldNumTimestamp,
			BaseType: "uint32", ProfileType: "date_time", Kind: "field", Raw: raw, pt: profile.DateTime})
	}
	for i := range out {
		fitProcess(&out[i])
	}
	if m.Num == mesgnum.FieldDescription {
		fd := mesgdef.NewFieldDescription(&m)
		name := fmt.Sprintf("unnamed_dev_field_%d", fd.FieldDefinitionNumber)
		if len(fd.FieldName) > 0 && fd.FieldName[0] != "" {
			name = strings.Clone(fd.FieldName[0])
		}
		units := ""
		if len(fd.Units) > 0 {
			units = strings.Clone(fd.Units[0])
		}
		d.dev[devFieldKey{fd.DeveloperDataIndex, fd.FieldDefinitionNumber}] = devFieldDesc{name, units, fd.FitBaseTypeId}
	}
	d.msgs = append(d.msgs, fitMessage{Name: fitMessageName(m.Num), Num: int(m.Num), Fields: out})
}

// appendField appends one defined field, preceded by its expanded components (fitparse order).
func (d *fitDecoder) appendField(out []fitField, m *proto.Message, f *proto.Field) []fitField {
	raw := fitRaw(f.Value, f.BaseType)
	if f.Name == factory.NameUnknown {
		bt := f.BaseType.String()
		return append(out, fitField{Name: fmt.Sprintf("unknown_%d", f.Num), Value: raw, Num: int(f.Num),
			BaseType: bt, ProfileType: bt, Kind: "field", Raw: raw, pt: f.Type})
	}
	ff := fitField{Name: f.Name, Units: f.Units, Num: int(f.Num), BaseType: f.BaseType.String(),
		ProfileType: f.Type.String(), Kind: "field", Raw: raw, pt: f.Type}
	scale, offset, comps := f.Scale, f.Offset, f.Components
	if sf := f.SubFieldSubstitution(m); sf != nil {
		ff.Name, ff.Units, ff.ProfileType, ff.pt, ff.Kind, ff.Parent = sf.Name, sf.Units, sf.Type.String(), sf.Type, "subfield", f.Name
		scale, offset, comps = sf.Scale, sf.Offset, sf.Components
	}
	bitOffset := 0
	for _, c := range comps {
		cmpRaw, ok := fitComponentRaw(raw, bitOffset, int(c.Bits))
		bitOffset += int(c.Bits)
		if !ok {
			continue
		}
		if n, isInt := cmpRaw.(int64); isInt && c.Accumulate {
			acc := d.accum[m.Num]
			if acc == nil {
				acc = map[byte]int64{}
				d.accum[m.Num] = acc
			}
			n = fitAccumulate(n, acc[c.FieldNum], int(c.Bits))
			acc[c.FieldNum] = n
			cmpRaw = n
		}
		cmpRaw = fitScale(cmpRaw, c.Scale, c.Offset)
		cf := factory.CreateField(m.Num, c.FieldNum)
		cmp := fitField{Name: cf.Name, Units: cf.Units, Num: int(cf.Num), BaseType: cf.BaseType.String(),
			ProfileType: cf.Type.String(), Kind: "field", Raw: cmpRaw, pt: cf.Type}
		if sf := cf.SubFieldSubstitution(m); sf != nil {
			cmp.Name, cmp.Units, cmp.ProfileType, cmp.pt, cmp.Kind, cmp.Parent = sf.Name, sf.Units, sf.Type.String(), sf.Type, "subfield", cf.Name
		}
		cmp.Value = fitRender(cmp.pt, cmpRaw) // a component keeps its own scale only
		out = append(out, cmp)
	}
	ff.Value = fitScale(fitRender(ff.pt, raw), scale, offset)
	return append(out, ff)
}

// fitComponentRaw is ComponentField.render: bits [offset, offset+bits) of the containing raw value.
// ok is false where fitparse skips the component (a byte array too short for the offset).
func fitComponentRaw(raw any, offset, bits int) (any, bool) {
	switch x := raw.(type) {
	case nil:
		return nil, true
	case []any: // byte array, unpacked little endian
		if offset != 0 && offset >= len(x)*8 {
			return nil, false
		}
		var n int64
		for k := 0; k < bits; k++ {
			pos := offset + k
			if pos/8 >= len(x) {
				break
			}
			b, _ := x[pos/8].(int64)
			n |= (b >> (pos % 8) & 1) << k
		}
		return n, true
	case int64:
		if offset >= 64 {
			return int64(0), true
		}
		return x >> offset & (1<<bits - 1), true
	case uint64:
		if offset >= 64 {
			return int64(0), true
		}
		return int64(x >> offset & (1<<bits - 1)), true
	}
	return raw, true
}

// fitAccumulate is _apply_compressed_accumulation.
func fitAccumulate(raw, accumulation int64, bits int) int64 {
	maxValue := int64(1) << bits
	mask := maxValue - 1
	base := raw + (accumulation &^ mask)
	if raw < accumulation&mask {
		base += maxValue
	}
	return base
}

// fitScale is _apply_scale_offset (elementwise on arrays). A scale of 1 and offset of 0 keep integers.
func fitScale(v any, scale, offset float64) any {
	if scale == 1 && offset == 0 {
		return v
	}
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = fitScale(x[i], scale, offset)
		}
		return out
	case int64, uint64, float64:
		f, _ := fitFloat(x)
		if scale != 0 {
			f /= scale
		}
		return f - offset
	}
	return v
}

func fitFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// fitInt is v as an integer key of a profile value table (fitparse dict lookup also matches 3.0 to 3).
func fitInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case uint64:
		return int64(x), x <= math.MaxInt64
	case float64:
		return int64(x), x == math.Trunc(x) && math.Abs(x) < 1<<62
	}
	return 0, false
}

// fitLabel is the profile type's label for raw (fitparse FieldType.values[raw]).
func fitLabel(pt profile.ProfileType, raw any) (string, bool) {
	n, ok := fitInt(raw)
	if !ok || pt <= profile.Bool || pt == profile.Invalid {
		return "", false
	}
	var v proto.Value
	switch bt := pt.BaseType(); bt {
	case basetype.Enum, basetype.Uint8, basetype.Uint8z, basetype.Byte:
		v = proto.Uint8(uint8(n))
	case basetype.Sint8:
		v = proto.Int8(int8(n))
	case basetype.Uint16, basetype.Uint16z:
		v = proto.Uint16(uint16(n))
	case basetype.Sint16:
		v = proto.Int16(int16(n))
	case basetype.Uint32, basetype.Uint32z:
		v = proto.Uint32(uint32(n))
	case basetype.Sint32:
		v = proto.Int32(int32(n))
	case basetype.Uint64, basetype.Uint64z:
		v = proto.Uint64(uint64(n))
	case basetype.Sint64:
		v = proto.Int64(n)
	default:
		return "", false
	}
	if back, _ := fitNumber(v.Any()); back != any(n) && back != any(uint64(n)) { // out of range: not a table key
		return "", false
	}
	s := printer.TypedefString(pt, v)
	if s == "" || strings.Contains(s, "Invalid(") {
		return "", false
	}
	return s, true
}

// fitRender is FieldAndSubFieldBase.render: the label when raw is a key of the type's table (never for arrays).
func fitRender(pt profile.ProfileType, raw any) any {
	if s, ok := fitLabel(pt, raw); ok {
		return s
	}
	return raw
}

// fitProcess runs fitparse's FitFileDataProcessor on one field.
func fitProcess(f *fitField) {
	if f.Value == nil {
		return
	}
	switch f.ProfileType {
	case "date_time":
		if n, ok := fitFloat(f.Value); ok && n >= 0x10000000 {
			f.Value, f.Units = fitTime(n), ""
		}
	case "local_date_time":
		if n, ok := fitFloat(f.Value); ok {
			f.Value, f.Units = fitTime(n), ""
		}
	case "localtime_into_day":
		if n, ok := fitInt(f.Value); ok && n >= 0 && n < 86400 { // datetime.time accepts no more
			f.Value, f.Units = fmt.Sprintf("%02d:%02d:%02d", n/3600, n/60%60, n%60), ""
		}
	case "bool":
		if n, ok := fitFloat(f.Value); ok {
			f.Value = n != 0
		} else if l, ok := f.Value.([]any); ok {
			f.Value = len(l) > 0
		}
	}
}

func fitTime(seconds float64) time.Time {
	sec, frac := math.Modf(seconds)
	return time.Unix(fitUTCReference+int64(sec), int64(math.Round(frac*1e6))*1e3).UTC()
}

// fitRaw converts a library value to fitparse's raw value: invalid values become nil (elementwise in arrays),
// a byte field is always an array (nil when all 0xFF), a one-element array is a scalar, strings stop at NUL.
func fitRaw(v proto.Value, bt basetype.BaseType) any {
	var elems []any
	switch x := v.Any().(type) {
	case nil:
		return nil
	case string:
		if x == "" {
			return nil
		}
		return strings.Clone(x)
	case []string:
		if len(x) == 0 || x[0] == "" {
			return nil
		}
		return strings.Clone(x[0])
	case []typedef.Bool:
		elems = fitElems(x)
	case []int8:
		elems = fitElems(x)
	case []uint8:
		elems = fitElems(x)
	case []int16:
		elems = fitElems(x)
	case []uint16:
		elems = fitElems(x)
	case []int32:
		elems = fitElems(x)
	case []uint32:
		elems = fitElems(x)
	case []int64:
		elems = fitElems(x)
	case []uint64:
		elems = fitElems(x)
	case []float32:
		elems = fitElems(x)
	case []float64:
		elems = fitElems(x)
	default:
		elems = []any{x}
		if bt != basetype.Byte {
			return fitElem(x, bt)
		}
	}
	if bt == basetype.Byte {
		out := make([]any, len(elems))
		all := true
		for i, e := range elems {
			n, _ := fitNumber(e)
			out[i] = n
			all = all && n == int64(0xFF)
		}
		if all {
			return nil
		}
		return out
	}
	if len(elems) == 0 {
		return nil
	}
	if len(elems) == 1 {
		return fitElem(elems[0], bt)
	}
	out := make([]any, len(elems))
	for i, e := range elems {
		out[i] = fitElem(e, bt)
	}
	return out
}

func fitElems[T any](s []T) []any {
	out := make([]any, len(s))
	for i := range s {
		out[i] = s[i]
	}
	return out
}

// fitNumber widens a library scalar: integers to int64 (uint64 stays uint64), floats to float64.
func fitNumber(x any) (any, bool) {
	switch n := x.(type) {
	case typedef.Bool:
		return int64(n), true
	case int8:
		return int64(n), true
	case uint8:
		return int64(n), true
	case int16:
		return int64(n), true
	case uint16:
		return int64(n), true
	case int32:
		return int64(n), true
	case uint32:
		return int64(n), true
	case int64:
		return n, true
	case uint64:
		return n, true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return x, false
}

// fitElem is BaseType.parse: the base type's invalid value becomes nil.
func fitElem(x any, bt basetype.BaseType) any {
	n, ok := fitNumber(x)
	if !ok {
		return nil
	}
	switch v := n.(type) {
	case float64:
		if math.IsNaN(v) {
			return nil
		}
	case uint64:
		if (bt == basetype.Uint64z && v == 0) || (bt != basetype.Uint64z && v == math.MaxUint64) {
			return nil
		}
	case int64:
		var invalid int64
		switch bt {
		case basetype.Enum, basetype.Uint8, basetype.Byte:
			invalid = 0xFF
		case basetype.Sint8:
			invalid = math.MaxInt8
		case basetype.Uint16:
			invalid = math.MaxUint16
		case basetype.Sint16:
			invalid = math.MaxInt16
		case basetype.Uint32:
			invalid = math.MaxUint32
		case basetype.Sint32:
			invalid = math.MaxInt32
		case basetype.Sint64:
			invalid = math.MaxInt64
		case basetype.Uint8z, basetype.Uint16z, basetype.Uint32z, basetype.Uint64z:
			invalid = 0
		default:
			return v
		}
		if v == invalid {
			return nil
		}
	}
	return n
}

// getField is _get_field: the first non-nil value among the named fields (fitparse get_value also matches
// a sub-field by its main field's name).
func getField(m fitMessage, names ...string) any {
	for _, name := range names {
		for _, f := range m.Fields {
			if f.Name == name || (f.Parent != "" && f.Parent == name) {
				if f.Value != nil {
					return f.Value
				}
				break
			}
		}
	}
	return nil
}

// semicirclesToDegrees is _semicircles_to_degrees.
func semicirclesToDegrees(v any) any {
	f, ok := fitFloat(v)
	if !ok {
		return nil
	}
	return round(f*(180.0/(1<<31)), 6)
}

// jsonSafeFitValue is _json_safe_fit_value. Python writes ±inf as Infinity, which encoding/json cannot.
func jsonSafeFitValue(v any) any {
	switch x := v.(type) {
	case []byte:
		return map[string]any{"encoding": "hex", "value": hex.EncodeToString(x)}
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = jsonSafeFitValue(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = jsonSafeFitValue(e)
		}
		return out
	case time.Time:
		if x.Nanosecond() != 0 {
			return x.Format("2006-01-02T15:04:05.000000")
		}
		return x.Format("2006-01-02T15:04:05")
	case float64:
		if math.IsInf(x, 1) {
			return "Infinity"
		}
		if math.IsInf(x, -1) {
			return "-Infinity"
		}
	}
	return v
}
