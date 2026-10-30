package server

import (
	"bytes"
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
	"math"
	"time"
)

func bodyCompositionFIT(date time.Time, args map[string]any) ([]byte, error) {
	weight, ok := args["weight"].(float64)
	if !ok || weight <= 0 || weight >= 655.35 || math.IsNaN(weight) || math.IsInf(weight, 0) {
		return nil, fmt.Errorf("weight must be a finite value in kg (not grams), at least 0 and below 655.35 for FIT encoding")
	}
	w := mesgdef.NewWeightScale(nil).SetTimestamp(date).SetWeightScaled(weight)
	fields := map[string]struct {
		scale float64
		bits  int
		set   func(float64)
	}{"percent_fat": {100, 16, func(v float64) { w.SetPercentFatScaled(v) }}, "percent_hydration": {100, 16, func(v float64) { w.SetPercentHydrationScaled(v) }}, "visceral_fat_mass": {100, 16, func(v float64) { w.SetVisceralFatMassScaled(v) }}, "bone_mass": {100, 16, func(v float64) { w.SetBoneMassScaled(v) }}, "muscle_mass": {100, 16, func(v float64) { w.SetMuscleMassScaled(v) }}, "basal_met": {4, 16, func(v float64) { w.SetBasalMetScaled(v) }}, "active_met": {4, 16, func(v float64) { w.SetActiveMetScaled(v) }}, "bmi": {10, 16, func(v float64) { w.SetBmiScaled(v) }}, "physique_rating": {1, 8, func(v float64) { w.SetPhysiqueRating(uint8(v)) }}, "metabolic_age": {1, 8, func(v float64) { w.SetMetabolicAge(uint8(v)) }}, "visceral_fat_rating": {1, 8, func(v float64) { w.SetVisceralFatRating(uint8(v)) }}}
	for name, field := range fields {
		if args[name] == nil {
			continue
		}
		v, ok := args[name].(float64)
		limit := float64((uint64(1) << field.bits) - 1)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v*field.scale >= limit {
			return nil, fmt.Errorf("%s is outside the valid FIT range", name)
		}
		field.set(v)
	}
	fileID := mesgdef.NewFileId(nil).SetType(typedef.File(9)).SetTimeCreated(time.Now())
	creator := proto.Message{Num: typedef.MesgNumFileCreator}
	software := factory.CreateField(typedef.MesgNumFileCreator, 0)
	software.Value = proto.Uint16(0xffff)
	hardware := factory.CreateField(typedef.MesgNumFileCreator, 1)
	hardware.Value = proto.Uint8(0xff)
	creator.Fields = []proto.Field{software, hardware}
	device := mesgdef.NewDeviceInfo(nil).SetTimestamp(date)
	fit := &proto.FIT{Messages: []proto.Message{fileID.ToMesg(nil), creator, device.ToMesg(nil), w.ToMesg(nil)}}
	var out bytes.Buffer
	if err := encoder.New(&out).Encode(fit); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func registerBodyCompositionWrite(srv *mcp.Server, m *manager) {
	schema := map[string]any{"date": map[string]any{"type": "string"}, "weight": map[string]any{"type": "number"}}
	for _, key := range []string{"percent_fat", "percent_hydration", "visceral_fat_mass", "bone_mass", "muscle_mass", "basal_met", "active_met", "physique_rating", "metabolic_age", "visceral_fat_rating", "bmi"} {
		schema[key] = map[string]any{"type": "number"}
	}
	addTool(srv, m, "add_body_composition", description("add_body_composition"), schema, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		date, err := dateArgument(args, "date")
		if err != nil {
			return nil, fmt.Errorf("Error adding body composition data: %w", err)
		}
		dt, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err != nil {
			return nil, err
		}
		data, err := bodyCompositionFIT(dt, args)
		if err != nil {
			return nil, fmt.Errorf("Error adding body composition data: %w", err)
		}
		tracker := newWriteTracker("add_body_composition", "Error adding body composition data", m.opt.Log)
		tracker.Sent()
		v, err := s.Client.Upload(ctx, "/upload-service/upload", "file", "body_composition.fit", data)
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return v, nil
	}, "date", "weight")
}
