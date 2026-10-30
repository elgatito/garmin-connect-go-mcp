package server

import (
	"context"
	"fmt"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

func registerWorkoutPush(srv *mcp.Server, m *manager) {
	addTool(srv, m, "push_workout_to_device", description("push_workout_to_device"), map[string]any{"workout_id": map[string]any{"type": "integer"}, "device_id": map[string]any{"type": "integer"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		workout, err := activityID(args["workout_id"])
		if err != nil {
			return nil, err
		}
		deviceRaw := args["device_id"]
		if deviceRaw == nil {
			v, err := s.Client.GetJSON(ctx, "/device-service/deviceservice/mylastused", nil)
			if err != nil {
				return nil, fmt.Errorf("Failed to push workout: %w", err)
			}
			d, _ := v.(map[string]any)
			deviceRaw = d["userDeviceId"]
			if !truthy(deviceRaw) {
				return map[string]any{"status": "failed", "workout_id": workout, "message": "No default device found. Pass device_id explicitly (see get_devices)."}, nil
			}
		}
		device, err := activityID(deviceRaw)
		if err != nil {
			return nil, err
		}
		v, err := s.Client.GetJSON(ctx, fmt.Sprintf("/workout-service/workout/%d", workout), nil)
		if err != nil {
			return nil, fmt.Errorf("Failed to push workout: %w", err)
		}
		data, _ := v.(map[string]any)
		name, _ := data["workoutName"].(string)
		if name == "" {
			name = fmt.Sprintf("Workout %d", workout)
		}
		payload := []any{map[string]any{"deviceId": device, "messageUrl": fmt.Sprintf("workout-service/workout/FIT/%d", workout), "messageType": "workouts", "groupName": nil, "messageName": name, "priority": 1, "fileType": "FIT", "metaDataId": workout}}
		tracker := newWriteTracker("push_workout_to_device", "Failed to push workout", m.opt.Log)
		tracker.Sent()
		_, err = s.Client.Do(ctx, garmin.Req{Method: http.MethodPost, Path: "/device-service/devicemessage/messages", JSON: payload})
		if err != nil {
			return nil, tracker.Fail(err)
		}
		tracker.Committed()
		return map[string]any{"status": "success", "workout_id": workout, "workout_name": name, "device_id": device, "message": fmt.Sprintf("Queued workout %d for device %d; it arrives on the device at its next sync", workout, device)}, nil
	}, "workout_id")
}
