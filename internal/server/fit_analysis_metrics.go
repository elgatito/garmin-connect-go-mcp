package server

import (
	"math"
	"sort"
	"strings"
	"time"
)

func fitHeldSeconds(records []map[string]any) []int {
	held := make([]int, len(records))
	for i := range held {
		held[i] = 1
	}
	if len(records) < 2 {
		return held
	}
	times := make([]time.Time, len(records))
	for i, r := range records {
		t, ok := parseFITTime(r["timestamp"])
		if !ok {
			return held
		}
		times[i] = t
	}
	gaps := make([]int, len(records)-1)
	for i := range gaps {
		gaps[i] = int(times[i+1].Sub(times[i]).Seconds())
		if gaps[i] < 0 {
			return held
		}
	}
	for i, g := range gaps {
		if g <= 10 {
			held[i] = g
		}
	}
	held[len(held)-1] = held[len(held)-2]
	return held
}
func deriveFITGrades(records []map[string]any) string {
	device := 0
	type point struct {
		idx       int
		dist, alt float64
	}
	points := []point{}
	for i, r := range records {
		if r["grade_pct"] != nil {
			device++
		}
		dist, dok := fitNumber(r["distance_m"])
		alt, aok := fitNumber(r["altitude_m"])
		if dok && aok {
			points = append(points, point{i, dist, alt})
		}
	}
	if len(records) > 0 && float64(device) >= 0.05*float64(len(records)) {
		return "device"
	}
	derived := 0
	lo, hi := 0, 0
	for _, p := range points {
		for lo < len(points)-1 && points[lo].dist < p.dist-50 {
			lo++
		}
		if hi < lo {
			hi = lo
		}
		for hi+1 < len(points) && points[hi+1].dist <= p.dist+50 {
			hi++
		}
		span := points[hi].dist - points[lo].dist
		if span < 20 || records[p.idx]["grade_pct"] != nil {
			continue
		}
		grade := (points[hi].alt - points[lo].alt) / span * 100
		records[p.idx]["grade_pct"] = rounded(math.Max(-40, math.Min(40, grade)), 100)
		derived++
	}
	if derived > 0 {
		return "derived"
	}
	if device > 0 {
		return "device"
	}
	return ""
}
func fitAverage(records []map[string]any, key string) (float64, bool) {
	sum, count := 0.0, 0
	for _, r := range records {
		if n, ok := fitNumber(r[key]); ok {
			sum += n
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}
func fitGradeAnalysis(records []map[string]any) map[string]any {
	type bin struct {
		records []map[string]any
		seconds int
	}
	bins := map[string]*bin{}
	held := fitHeldSeconds(records)
	for i, r := range records {
		grade, ok := fitNumber(r["grade_pct"])
		if !ok {
			continue
		}
		key := "steep"
		if grade < -3 {
			key = "descending"
		} else if grade < 3 {
			key = "flat"
		} else if grade < 6 {
			key = "gentle"
		} else if grade < 9 {
			key = "moderate"
		}
		if bins[key] == nil {
			bins[key] = &bin{}
		}
		bins[key].records = append(bins[key].records, r)
		bins[key].seconds += held[i]
	}
	out := map[string]any{}
	for key, b := range bins {
		v := map[string]any{"time_s": b.seconds}
		for dst, src := range map[string]string{"avg_power_w": "power_w", "avg_cadence_rpm": "cadence_rpm", "avg_hr_bpm": "heart_rate_bpm"} {
			if avg, ok := fitAverage(b.records, src); ok {
				v[dst] = rounded(avg, 1)
			}
		}
		out[key] = v
	}
	return out
}
func fitHRDrift(records []map[string]any) map[string]any {
	held := fitHeldSeconds(records)
	filtered := []map[string]any{}
	seconds := 0
	for i, r := range records {
		_, pok := fitNumber(r["power_w"])
		hr, hok := fitNumber(r["heart_rate_bpm"])
		if pok && hok && hr > 0 {
			filtered = append(filtered, r)
			seconds += held[i]
		}
	}
	if seconds < 3600 || len(filtered) < 2 {
		return nil
	}
	mid := len(filtered) / 2
	p1, _ := fitAverage(filtered[:mid], "power_w")
	h1, _ := fitAverage(filtered[:mid], "heart_rate_bpm")
	p2, _ := fitAverage(filtered[mid:], "power_w")
	h2, _ := fitAverage(filtered[mid:], "heart_rate_bpm")
	if h1 == 0 || h2 == 0 || p1 == 0 {
		return nil
	}
	r1, r2 := p1/h1, p2/h2
	drift := (r2 - r1) / r1 * 100
	interpretation := "significant_decoupling"
	if math.Abs(drift) < 5 {
		interpretation = "well_coupled"
	} else if drift > 0 {
		interpretation = "power_increased_vs_hr"
	} else if drift > -10 {
		interpretation = "moderate_drift"
	}
	return map[string]any{"hr_drift_pct": rounded(drift, 10), "first_half_power_hr_ratio": rounded(r1, 1000), "second_half_power_hr_ratio": rounded(r2, 1000), "interpretation": interpretation, "note": "Negative drift = HR increased vs power (decoupling); below -10% suggests aerobic base is limiting factor. Positive drift = power increased vs HR (not decoupling)."}
}
func fitTemperature(records []map[string]any) map[string]any {
	temps := []map[string]any{}
	for _, r := range records {
		if _, ok := fitNumber(r["temperature_c"]); ok {
			temps = append(temps, r)
		}
	}
	if len(temps) == 0 {
		return nil
	}
	sort.SliceStable(temps, func(i, j int) bool {
		a, _ := fitNumber(temps[i]["temperature_c"])
		b, _ := fitNumber(temps[j]["temperature_c"])
		return a < b
	})
	min, _ := fitNumber(temps[0]["temperature_c"])
	max, _ := fitNumber(temps[len(temps)-1]["temperature_c"])
	avg, _ := fitAverage(temps, "temperature_c")
	out := map[string]any{"avg_temp_c": rounded(avg, 10), "min_temp_c": min, "max_temp_c": max, "temp_range_c": rounded(max-min, 10)}
	third := len(temps) / 3
	if third < 1 {
		third = 1
	}
	for label, rows := range map[string][]map[string]any{"coolest": temps[:third], "hottest": temps[len(temps)-third:]} {
		for dst, src := range map[string]string{"avg_hr_" + label + "_third_bpm": "heart_rate_bpm", "avg_power_" + label + "_third_w": "power_w"} {
			if v, ok := fitAverage(rows, src); ok {
				out[dst] = rounded(v, 10)
			}
		}
	}
	return out
}
func fitClimbs(records []map[string]any) []map[string]any {
	held := fitHeldSeconds(records)
	climbs := []map[string]any{}
	active := false
	start, flatCount, flatSeconds := 0, 0, 0
	summarize := func(begin, end int) {
		if end < begin {
			return
		}
		segment := records[begin : end+1]
		duration := 0
		for _, h := range held[begin : end+1] {
			duration += h
		}
		a, aok := fitNumber(segment[0]["altitude_m"])
		b, bok := fitNumber(segment[len(segment)-1]["altitude_m"])
		if !aok || !bok || b-a < 50 || duration < 120 {
			return
		}
		gain := b - a
		out := map[string]any{"start_time": fitString(segment[0]["timestamp"]), "end_time": fitString(segment[len(segment)-1]["timestamp"]), "duration_s": duration, "elevation_gain_m": rounded(gain, 10), "vam_m_per_hr": rounded(gain/float64(duration)*3600, 1)}
		d1, ok1 := fitNumber(segment[0]["distance_m"])
		d2, ok2 := fitNumber(segment[len(segment)-1]["distance_m"])
		if ok1 && ok2 {
			out["distance_m"] = rounded(d2-d1, 1)
		} else {
			dist := 0.0
			count := 0
			for i, r := range segment {
				if speed, ok := fitNumber(r["speed_mps"]); ok {
					dist += speed * float64(held[begin+i])
					count++
				}
			}
			if count > 0 {
				out["distance_m"] = rounded(dist, 1)
			}
		}
		for dst, src := range map[string]string{"avg_grade_pct": "grade_pct", "avg_power_w": "power_w", "avg_cadence_rpm": "cadence_rpm", "avg_hr_bpm": "heart_rate_bpm"} {
			if avg, ok := fitAverage(segment, src); ok {
				scale := 1.0
				if src == "grade_pct" {
					scale = 10
				}
				out[dst] = rounded(avg, scale)
			}
		}
		max := math.Inf(-1)
		for _, r := range segment {
			if grade, ok := fitNumber(r["grade_pct"]); ok && grade > max {
				max = grade
			}
		}
		if !math.IsInf(max, -1) {
			out["max_grade_pct"] = rounded(max, 10)
		}
		climbs = append(climbs, out)
	}
	for i, r := range records {
		grade, ok := fitNumber(r["grade_pct"])
		if !ok {
			continue
		}
		if !active {
			if grade >= 3 {
				active = true
				start = i
				flatCount = 0
				flatSeconds = 0
			}
		} else if grade < 3 {
			flatCount++
			flatSeconds += held[i]
			if flatSeconds > 30 {
				summarize(start, i-flatCount)
				active = false
				flatCount = 0
				flatSeconds = 0
			}
		} else {
			flatCount = 0
			flatSeconds = 0
		}
	}
	if active {
		summarize(start, len(records)-1)
	}
	return climbs
}
func fitHRVMetrics(rr []rrPair) map[string]any {
	if len(rr) < 10 {
		return nil
	}
	mean := 0.0
	for _, r := range rr {
		mean += r.value * 1000
	}
	mean /= float64(len(rr))
	variance, squares := 0.0, 0.0
	nn50 := 0
	for i, r := range rr {
		n := r.value * 1000
		variance += (n - mean) * (n - mean)
		if i > 0 {
			d := n - rr[i-1].value*1000
			squares += d * d
			if math.Abs(d) > 50 {
				nn50++
			}
		}
	}
	return map[string]any{"rmssd_ms": rounded(math.Sqrt(squares/float64(len(rr)-1)), 10), "sdnn_ms": rounded(math.Sqrt(variance/float64(len(rr)-1)), 10), "pnn50_pct": rounded(100*float64(nn50)/float64(len(rr)-1), 100), "mean_rr_ms": rounded(mean, 10), "mean_hr_bpm": rounded(60000/mean, 10), "rr_count": len(rr)}
}
func fitShiftSummary(shifts []map[string]any) map[string]any {
	if len(shifts) == 0 {
		return map[string]any{"total_shifts": 0, "note": "No DI2 shift events found in FIT file"}
	}
	counts := map[string]int{}
	terrain := map[string]map[string]int{}
	gear := map[string]int{}
	front, rear := 0, 0
	cadences := []float64{}
	times := []time.Time{}
	hasTerrain := false
	for _, s := range shifts {
		quality := fitString(s["quality"])
		counts[quality]++
		event := fitString(s["event"])
		if strings.Contains(event, "front") {
			front++
		} else {
			rear++
		}
		if combo := fitString(s["gear_combo"]); combo != "" {
			gear[combo]++
		}
		if n, ok := fitNumber(s["cadence_at_shift_rpm"]); ok {
			cadences = append(cadences, n)
		}
		if t, ok := parseFITTime(s["timestamp"]); ok {
			times = append(times, t)
		}
		category := "flat"
		if grade, ok := fitNumber(s["grade_at_shift_pct"]); ok {
			hasTerrain = true
			if grade >= 3 {
				category = "climbing"
			} else if grade <= -3 {
				category = "descending"
			}
		}
		if terrain[category] == nil {
			terrain[category] = map[string]int{}
		}
		terrain[category][quality]++
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	bursts := 0
	for i := 0; i < len(times); {
		end := i
		for end+1 < len(times) && times[end+1].Sub(times[i]) <= 10*time.Second {
			end++
		}
		if end-i+1 >= 3 {
			bursts++
			i = end + 1
		} else {
			i++
		}
	}
	n := len(shifts)
	out := map[string]any{"total_shifts": n, "front_shifts": front, "rear_shifts": rear, "proactive_shifts": counts["proactive"], "reactive_shifts": counts["reactive"], "coasting_shifts": counts["coasting"], "spun_out_shifts": counts["spun_out"], "proactive_pct": rounded(float64(counts["proactive"])/float64(n)*100, 10), "reactive_pct": rounded(float64(counts["reactive"])/float64(n)*100, 10), "panic_burst_episodes": bursts, "gear_usage": gear}
	if hasTerrain {
		byTerrain := map[string]any{}
		for key, c := range terrain {
			total := 0
			for _, n := range c {
				total += n
			}
			byTerrain[key] = map[string]any{"total": total, "proactive": c["proactive"], "reactive": c["reactive"], "reactive_pct": rounded(float64(c["reactive"])/float64(total)*100, 10)}
		}
		out["by_terrain"] = byTerrain
	}
	if len(cadences) > 0 {
		sort.Float64s(cadences)
		sum := 0.0
		for _, c := range cadences {
			sum += c
		}
		out["avg_cadence_at_shift_rpm"] = rounded(sum/float64(len(cadences)), 10)
		out["min_cadence_at_shift_rpm"] = cadences[0]
		out["max_cadence_at_shift_rpm"] = cadences[len(cadences)-1]
	}
	return out
}
