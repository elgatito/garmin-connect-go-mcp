package server

// Port of activity_analysis._serialize_fit_field … _parse_fit_messages: every message and field of a FIT file,
// with the high-frequency inventory and pagination of get_activity_fit_messages.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/muktihari/fit/profile"
)

// ordered is a JSON object that keeps insertion order, like a Python dict.
type ordered[V any] struct {
	keys []string
	vals map[string]V
}

func newOrdered[V any]() *ordered[V] { return &ordered[V]{vals: map[string]V{}} }

func (o *ordered[V]) set(k string, v V) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *ordered[V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type fitFieldJSON struct {
	Name        string  `json:"name"`
	Value       any     `json:"value"`
	Units       *string `json:"units,omitempty"`
	DefNum      int     `json:"definition_number"`
	BaseType    string  `json:"base_type,omitempty"`
	ProfileType string  `json:"profile_type,omitempty"`
	FieldType   string  `json:"field_type,omitempty"`
	ValueName   any     `json:"value_name,omitempty"`
	ValueNames  []any   `json:"value_names,omitempty"`
	RawValue    any     `json:"raw_value"`
}

type fitMessageJSON struct {
	MessageIndex        int            `json:"message_index"`
	TypeIndex           int            `json:"type_index"`
	Type                string         `json:"type"`
	Fields              []fitFieldJSON `json:"fields"`
	GlobalMessageNumber int            `json:"global_message_number"`
}

type fitRecordStream struct {
	Included      bool   `json:"included"`
	TotalCount    int    `json:"total_count"`
	ReturnedCount int    `json:"returned_count"`
	Hint          string `json:"hint,omitempty"`
}

type fitPagination struct {
	TotalEligibleCount int  `json:"total_eligible_count"`
	ReturnedCount      int  `json:"returned_count"`
	Offset             int  `json:"offset"`
	Limit              int  `json:"limit"`
	NextOffset         *int `json:"next_offset,omitempty"`
}

type fitOmittedType struct {
	Count int    `json:"count"`
	Hint  string `json:"hint"`
}

type fitMessagesResult struct {
	FitSizeBytes          int                      `json:"fit_size_bytes"`
	MessageCounts         *ordered[int]            `json:"message_counts"`
	ReturnedMessageCounts *ordered[int]            `json:"returned_message_counts"`
	RecordStream          fitRecordStream          `json:"record_stream"`
	Pagination            fitPagination            `json:"pagination"`
	Messages              []fitMessageJSON         `json:"messages"`
	Omitted               *ordered[fitOmittedType] `json:"omitted_high_frequency_message_types,omitempty"`
}

// fitProfileLabel is _fit_profile_label: the label of an integer raw or decoded value, or the decoded value when
// fitparse already rendered it as the label.
func fitProfileLabel(value, raw any, pt profile.ProfileType) any {
	for _, c := range []any{raw, value} {
		switch c.(type) {
		case int64, uint64:
			if s, ok := fitLabel(pt, c); ok {
				return s
			}
		}
	}
	if s, ok := value.(string); ok {
		if l, ok := fitLabel(pt, raw); ok && l == s {
			return s
		}
	}
	return nil
}

// fitMapLabels is _map_fit_profile_values: a label, or a list of labels when either value is an array.
func fitMapLabels(value, raw any, pt profile.ProfileType) (any, bool) {
	vs, vSeq := value.([]any)
	rs, rSeq := raw.([]any)
	if !vSeq && !rSeq {
		l := fitProfileLabel(value, raw, pt)
		return l, l != nil
	}
	if !vSeq {
		vs = []any{value}
	}
	if !rSeq {
		rs = []any{raw}
	}
	out := make([]any, max(len(vs), len(rs)))
	has := false
	for i := range out {
		out[i] = fitProfileLabel(fitAt(vs, i), fitAt(rs, i), pt)
		has = has || out[i] != nil
	}
	return out, has
}

func fitAt(s []any, i int) any {
	if i < len(s) {
		return s[i]
	}
	return nil
}

// fitContextualEnums is _FIT_CONTEXTUAL_ENUM_FIELDS: exercise names whose type depends on a sibling category.
var fitContextualEnums = map[[2]string]string{
	{"set", "category_subtype"}:         "category",
	{"exercise_title", "exercise_name"}: "exercise_category",
}

// fitContextualNames is _contextual_fit_value_names.
func fitContextualNames(f fitField, m fitMessage) (any, bool) {
	selectorName, ok := fitContextualEnums[[2]string{strings.ToLower(m.Name), strings.ToLower(f.Name)}]
	if !ok {
		return nil, false
	}
	var selector *fitField
	for i := range m.Fields {
		if strings.ToLower(m.Fields[i].Name) == selectorName {
			selector = &m.Fields[i]
			break
		}
	}
	if selector == nil {
		return nil, false
	}
	names, ok := fitMapLabels(selector.Value, selector.Raw, profile.ExerciseCategory)
	if !ok {
		return nil, false
	}
	selectors, isList := names.([]any)
	if !isList {
		selectors = []any{names}
	}
	vs, vSeq := f.Value.([]any)
	rs, rSeq := f.Raw.([]any)
	if !vSeq {
		vs = []any{f.Value}
	}
	if !rSeq {
		rs = []any{f.Raw}
	}
	out := make([]any, max(len(vs), len(rs)))
	has := false
	for i := range out {
		pt := profile.Invalid
		if i < len(selectors) {
			if s, ok := selectors[i].(string); ok {
				pt = profile.ProfileTypeFromString(s + "_exercise_name")
			}
		}
		out[i] = fitProfileLabel(fitAt(vs, i), fitAt(rs, i), pt)
		has = has || out[i] != nil
	}
	if !vSeq && !rSeq {
		return out[0], has
	}
	return out, has
}

// serializeFitField is _serialize_fit_field.
func serializeFitField(f fitField, m fitMessage) fitFieldJSON {
	out := fitFieldJSON{Name: f.Name, Value: jsonSafeFitValue(f.Value), DefNum: f.Num, BaseType: f.BaseType,
		ProfileType: f.ProfileType, FieldType: f.Kind, RawValue: jsonSafeFitValue(f.Raw)}
	if f.Units != "" {
		units := f.Units
		out.Units = &units
	}
	names, ok := fitMapLabels(f.Value, f.Raw, f.pt)
	if !ok {
		names, ok = fitContextualNames(f, m)
	}
	if ok {
		if l, isList := names.([]any); isList {
			out.ValueNames = l
		} else {
			out.ValueName = names
		}
	}
	return out
}

// parseFitMessages is _parse_fit_messages.
func parseFitMessages(raw []byte, messageTypes []string, includeRecords bool, messageOffset, messageLimit int) (*fitMessagesResult, error) {
	if messageOffset < 0 {
		return nil, errors.New("message_offset must be at least 0")
	}
	if messageLimit < 1 || messageLimit > 5000 {
		return nil, errors.New("message_limit must be between 1 and 5000")
	}
	var selected map[string]bool
	if messageTypes != nil {
		selected = map[string]bool{}
		for _, t := range messageTypes {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				selected[t] = true
			}
		}
	}
	recordsSelected := selected == nil || selected["record"]

	fitBytes, err := extractFitBytes(raw)
	if err != nil {
		return nil, err
	}
	msgs, err := decodeFIT(fitBytes)
	if err != nil {
		return nil, err
	}
	counts := newOrdered[int]()
	for _, m := range msgs {
		counts.set(m.Name, counts.vals[m.Name]+1)
	}

	omitted := map[string]bool{}
	if selected == nil {
		for _, t := range counts.keys {
			if strings.ToLower(t) != "record" && counts.vals[t] > 100 {
				omitted[t] = true
			}
		}
	}
	eligible := func(t string) bool {
		lt := strings.ToLower(t)
		if lt == "record" && (!includeRecords || !recordsSelected) {
			return false
		}
		if selected != nil && !selected[lt] {
			return false
		}
		return !omitted[t]
	}
	total := 0
	for _, t := range counts.keys {
		if eligible(t) {
			total += counts.vals[t]
		}
	}

	pageEnd := messageOffset + messageLimit
	messages := []fitMessageJSON{}
	eligibleIndex := 0
	seen := map[string]int{}
	for i, m := range msgs {
		typeIndex := seen[m.Name]
		seen[m.Name] = typeIndex + 1
		if !eligible(m.Name) {
			continue
		}
		if messageOffset <= eligibleIndex && eligibleIndex < pageEnd {
			fields := make([]fitFieldJSON, len(m.Fields))
			for j, f := range m.Fields {
				fields[j] = serializeFitField(f, m)
			}
			messages = append(messages, fitMessageJSON{MessageIndex: i, TypeIndex: typeIndex, Type: m.Name,
				Fields: fields, GlobalMessageNumber: m.Num})
		}
		eligibleIndex++
		if eligibleIndex >= pageEnd {
			break
		}
	}

	returned := newOrdered[int]()
	for _, m := range messages {
		returned.set(m.Type, returned.vals[m.Type]+1)
	}
	recordCount, returnedRecords := 0, 0
	for _, t := range counts.keys {
		if strings.ToLower(t) == "record" {
			recordCount += counts.vals[t]
		}
	}
	for _, t := range returned.keys {
		if strings.ToLower(t) == "record" {
			returnedRecords += returned.vals[t]
		}
	}
	res := &fitMessagesResult{
		FitSizeBytes:          len(fitBytes),
		MessageCounts:         counts,
		ReturnedMessageCounts: returned,
		RecordStream:          fitRecordStream{Included: includeRecords && recordsSelected, TotalCount: recordCount, ReturnedCount: returnedRecords},
		Pagination:            fitPagination{TotalEligibleCount: total, ReturnedCount: len(messages), Offset: messageOffset, Limit: messageLimit},
		Messages:              messages,
	}
	if !includeRecords && recordsSelected && recordCount > 0 {
		res.RecordStream.Hint = "Set include_records=true and use message pagination to retrieve records."
	}
	if next := messageOffset + len(messages); next < total {
		res.Pagination.NextOffset = &next
	}
	if len(omitted) > 0 {
		res.Omitted = newOrdered[fitOmittedType]()
		for _, t := range counts.keys {
			if omitted[t] {
				res.Omitted.set(t, fitOmittedType{counts.vals[t], fmt.Sprintf("Request message_types=['%s'] and follow pagination.", t)})
			}
		}
	}
	return res, nil
}
