package server

import "testing"

func TestActivityWeatherUnits(t *testing.T) {
	w := map[string]any{"temp": float64(68), "windSpeed": float64(12)}
	metric := activityWeather(1, w, "metric")
	us := activityWeather(1, w, "statute_us")
	if metric["temperature"] != float64(20) || metric["wind_speed"] != float64(12) || metric["wind_speed_unit"] != "km/h" || us["temperature"] != float64(68) || us["wind_speed_unit"] != "mph" {
		t.Fatal(metric, us)
	}
}
