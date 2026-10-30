package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testAnalysisClient(t *testing.T, handler http.HandlerFunc) *mcp.ClientSession {
	t.Helper()
	fake := httptest.NewServer(handler)
	t.Cleanup(fake.Close)
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}
func callAnalysisTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	args["email"] = "me@x"
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s: %v %v", name, result, err)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &data); err != nil {
		t.Fatal(err)
	}
	return data
}
func TestEnergyBalanceRegressionAndChunking(t *testing.T) {
	calls := 0
	cs := testAnalysisClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nutrition-service/food/logs/range":
			fmt.Fprint(w, `{"dailyNutritionSummaries":[{"mealDate":"2024-01-02","dailyNutritionContent":{"calories":2000},"mealDetails":[{"loggedFoods":[{},{}]}]}]}`)
		case "/weight-service/weight/dateRange":
			fmt.Fprint(w, `{"dateWeightList":[{"calendarDate":"2024-01-01","weight":80000,"bodyFat":20},{"calendarDate":"2024-01-25","weight":79000,"bodyFat":18.75}]}`)
		default:
			if r.URL.Query().Get("statsType") != "CALORIES" {
				t.Errorf("unexpected request: %s", r.URL)
			}
			calls++
			fmt.Fprint(w, `{"values":[{"calendarDate":"2024-01-01","values":{"totalCalories":2500}}]}`)
		}
	})
	result := callAnalysisTool(t, cs, "get_energy_balance", map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-29"})
	if calls != 2 {
		t.Fatalf("calorie chunks: %d", calls)
	}
	derived := asMap(result["derived"])
	if math.Abs(derived["composition_derived_tdee_kcal_per_day"].(float64)-2366.9) > 0.11 {
		t.Fatal(result)
	}
	if _, ok := result["gate_note"]; ok {
		t.Fatal(result)
	}
	if len(slice(asMap(result["intake"])["low_item_count_dates"])) != 1 {
		t.Fatal(result)
	}
}
func TestBodyCompGate(t *testing.T) {
	if bodyCompGate([]float64{0, 10}) == "" || bodyCompGate([]float64{0, 1, 24}) == "" || bodyCompGate([]float64{0, 24}) != "" {
		t.Fatal("body composition gate incorrect")
	}
}
func TestTrainingStatusRejectsCrossDeviceLoad(t *testing.T) {
	cs := testAnalysisClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case devicePath:
			fmt.Fprint(w, `[{"deviceId":1,"primaryDevice":true},{"deviceId":2,"displayName":"Watch"}]`)
		case trainingStatusPath + "2024-01-01":
			fmt.Fprint(w, `{"mostRecentTrainingStatus":{"latestTrainingStatusData":{"2":{"deviceId":2,"acuteTrainingLoadDTO":{"dailyTrainingLoadAcute":999,"dailyTrainingLoadChronic":999}}}},"mostRecentTrainingLoadBalance":{"metricsTrainingLoadBalanceDTOMap":{"1":{"deviceId":1,"monthlyLoadAerobicLow":500}}}}`)
		case "/metrics-service/metrics/trainingreadiness/2024-01-01":
			fmt.Fprint(w, `[{"deviceId":1,"calendarDate":"2024-01-01","acuteLoad":42}]`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	})
	result := callAnalysisTool(t, cs, "get_training_status", map[string]any{"date": "2024-01-01"})
	if result["acute_load"] != float64(42) || result["chronic_load"] != nil || result["monthly_load_aerobic_low"] != nil || result["load_unavailable_reason"] != "load_device_mismatch" {
		t.Fatal(result)
	}
}
func TestVO2TrendCarriesMeasurements(t *testing.T) {
	cs := testAnalysisClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics-service/metrics/maxmet/daily/2024-01-01/2024-01-03" {
			fmt.Fprint(w, `[{"calendarDate":"2024-01-01","generic":{"vo2MaxPreciseValue":42.1}},{"calendarDate":"2024-01-03","generic":{"vo2MaxPreciseValue":43.2}}]`)
		} else if r.URL.Path == trainingStatusPath+"2024-01-02" {
			fmt.Fprint(w, `{}`)
		} else {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	})
	result := callAnalysisTool(t, cs, "get_vo2max_trend", map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-03"})
	trend := slice(result["trend"])
	if len(trend) != 3 || asMap(trend[1])["carried_forward"] != true || result["change"] != float64(1.1) {
		t.Fatal(result)
	}
}
func testFITMessage(name string, fields map[string]any) fitMessage {
	msg := fitMessage{Name: name}
	for k, v := range fields {
		msg.Fields = append(msg.Fields, fitField{Name: k, Value: v})
	}
	return msg
}
func TestFITAnalysisCuratesAndKeepsMultisportSeparate(t *testing.T) {
	messages := []fitMessage{testFITMessage("session", map[string]any{"sport": float64(1), "start_time": "2024-01-01T00:00:00", "total_elapsed_time": float64(10)}), testFITMessage("session", map[string]any{"sport": float64(2), "start_time": "2024-01-01T00:00:10", "total_elapsed_time": float64(10), "avg_power": float64(200), "normalized_power": float64(220), "left_right_balance": float64(4700)}), testFITMessage("record", map[string]any{"timestamp": "2024-01-01T00:00:00", "power": float64(400)}), testFITMessage("record", map[string]any{"timestamp": "2024-01-01T00:00:10", "power": float64(100), "cadence": float64(65)}), testFITMessage("record", map[string]any{"timestamp": "2024-01-01T00:00:15", "power": float64(100)}), testFITMessage("event", map[string]any{"event": "rear_gear_change", "timestamp": "2024-01-01T00:00:16", "data": float64(0x32110b01)})}
	result := parseFITAnalysis(messages, true)
	session := asMap(result["session"])
	if result["analyzed_session_index"] != 1 || session["sport"] != "cycling" || session["variability_index"] != 1.1 || session["avg_left_power_pct"] != float64(47) {
		t.Fatal(result)
	}
	curve := result["power_duration_curve"].(map[string]int)
	if curve["5s"] != 100 {
		t.Fatal(curve)
	}
	if asMap(result["shift_summary"])["reactive_shifts"] != 1 {
		t.Fatal(result)
	}
}

func TestFractionalCallTimeout(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer fake.Close()
	store := accounts.Store{Base: t.TempDir()}
	if err := store.SaveTokens("me@x", garmin.Tokens{DIToken: "token"}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{Store: store, Version: "test", Endpoints: func(string) garmin.Endpoints { return garmin.Endpoints{ConnectAPI: fake.URL} }, Getenv: func(k string) string {
		if k == "GARMIN_MCP_CALL_TIMEOUT" {
			return "0.05"
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_floors", Arguments: map[string]any{"email": "me@x", "date": "2024-01-01"}})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "within 0.05s") {
		t.Fatal(res, err)
	}
}

func TestProfileEmptyResponseUsesMessage(t *testing.T) {
	cs := testAnalysisClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_user_profile", Arguments: map[string]any{"email": "me@x"}})
	if err != nil || result.IsError || result.Content[0].(*mcp.TextContent).Text != "No user profile information found." {
		t.Fatal(result, err)
	}
}
