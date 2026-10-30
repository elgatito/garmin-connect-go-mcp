package server

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/url"
	"sort"
	"time"
)

type linearFit struct {
	slope, intercept float64
	se               any
}

func fitLine(xs, ys []float64) *linearFit {
	n := float64(len(xs))
	if n < 2 {
		return nil
	}
	mx, my := 0.0, 0.0
	for i, x := range xs {
		mx += x
		my += ys[i]
	}
	mx /= n
	my /= n
	sxx, sxy := 0.0, 0.0
	for i, x := range xs {
		sxx += (x - mx) * (x - mx)
		sxy += (x - mx) * (ys[i] - my)
	}
	if sxx == 0 {
		return nil
	}
	slope := sxy / sxx
	fit := &linearFit{slope: slope, intercept: my - slope*mx}
	if n > 2 {
		sse := 0.0
		for i, x := range xs {
			d := ys[i] - (slope*x + fit.intercept)
			sse += d * d
		}
		fit.se = math.Sqrt(sse / (n - 2) / sxx)
	}
	return fit
}
func bodyCompGate(xs []float64) string {
	span := xs[len(xs)-1] - xs[0]
	if span < 21 {
		return fmt.Sprintf("readings span only %.0f days; need at least 21 to separate a real trend from water-weight noise", span)
	}
	n := float64(len(xs))
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= n
	sxx := 0.0
	for _, x := range xs {
		sxx += (x - mean) * (x - mean)
	}
	if sxx == 0 {
		return "all readings fall on the same day"
	}
	maxCluster, total := 0.0, 0.0
	for _, x := range xs {
		total += 1/n + (x-mean)*(x-mean)/sxx
	}
	for _, anchor := range xs {
		cluster := 0.0
		for _, x := range xs {
			if x >= anchor && x-anchor <= 2 {
				cluster += 1/n + (x-mean)*(x-mean)/sxx
			}
		}
		maxCluster = math.Max(maxCluster, cluster)
	}
	if maxCluster > total/2+1e-12 {
		return fmt.Sprintf("a single cluster of readings within 48h of each other accounts for %.0f%% of the fit's statistical leverage -- the trend across the full window is not reliably distinguishable from noise in that cluster alone", 100*maxCluster/total)
	}
	return ""
}
func registerEnergyBalance(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_energy_balance", description("get_energy_balance"), map[string]any{"start_date": map[string]any{"type": "string"}, "end_date": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		start, e := dateArgument(args, "start_date")
		if e != nil {
			return nil, e
		}
		end, e := dateArgument(args, "end_date")
		if e != nil {
			return nil, e
		}
		a, _ := time.Parse("2006-01-02", start)
		b, _ := time.Parse("2006-01-02", end)
		days := int(b.Sub(a).Hours()/24) + 1
		if days < 1 {
			return "end_date must be on or after start_date.", nil
		}
		if days > 61 {
			return fmt.Sprintf("Date range too large (%d days). Maximum is 61 days.", days), nil
		}
		intake, e := s.Client.GetJSON(ctx, "/nutrition-service/food/logs/range", url.Values{"startDate": {start}, "endDate": {end}})
		if e != nil {
			return nil, fmt.Errorf("Error retrieving energy balance data: %w", e)
		}
		sumIn, countIn := 0.0, 0
		low := []string{}
		for _, x := range slice(asMap(intake)["dailyNutritionSummaries"]) {
			day := asMap(x)
			date, _ := day["mealDate"].(string)
			if date == "" {
				continue
			}
			count := 0
			for _, meal := range slice(day["mealDetails"]) {
				count += len(slice(asMap(meal)["loggedFoods"]))
			}
			cal, ok := asMap(day["dailyNutritionContent"])["calories"].(float64)
			if count > 0 && ok {
				sumIn += cal
				countIn++
			}
			if count > 0 && count < 3 {
				low = append(low, date)
			}
		}
		sort.Strings(low)
		sumEx, countEx := 0.0, 0
		today := time.Now().Format("2006-01-02")
		for from := a; !from.After(b); from = from.AddDate(0, 0, 28) {
			to := from.AddDate(0, 0, 27)
			if to.After(b) {
				to = b
			}
			path := "/usersummary-service/stats/daily/" + from.Format("2006-01-02") + "/" + to.Format("2006-01-02")
			v, e := s.Client.GetJSON(ctx, path, url.Values{"statsType": {"CALORIES"}})
			if e != nil {
				return nil, fmt.Errorf("Error retrieving energy balance data: %w", e)
			}
			for _, x := range slice(asMap(v)["values"]) {
				entry := asMap(x)
				if entry["calendarDate"] == today {
					continue
				}
				if cal, ok := asMap(entry["values"])["totalCalories"].(float64); ok {
					sumEx += cal
					countEx++
				}
			}
		}
		body, e := s.Client.GetJSON(ctx, "/weight-service/weight/dateRange", url.Values{"startDate": {start}, "endDate": {end}})
		if e != nil {
			return nil, fmt.Errorf("Error retrieving energy balance data: %w", e)
		}
		type reading struct {
			date        string
			weight, fat float64
		}
		readings := []reading{}
		for _, v := range slice(asMap(body)["dateWeightList"]) {
			r := asMap(v)
			date, _ := r["calendarDate"].(string)
			w, wok := r["weight"].(float64)
			fat, fok := r["bodyFat"].(float64)
			if date != "" && wok && fok {
				if _, e := time.Parse("2006-01-02", date); e != nil {
					return nil, fmt.Errorf("Error retrieving energy balance data: invalid reading date %q", date)
				}
				readings = append(readings, reading{date, w / 1000, fat})
			}
		}
		sort.SliceStable(readings, func(i, j int) bool { return readings[i].date < readings[j].date })
		var meanIn, meanEx any
		if countIn > 0 {
			meanIn = math.RoundToEven(sumIn/float64(countIn)*10) / 10
		}
		if countEx > 0 {
			meanEx = math.RoundToEven(sumEx/float64(countEx)*10) / 10
		}
		if countIn == 0 && countEx == 0 && len(readings) == 0 {
			return fmt.Sprintf("No usable data found between %s and %s.", start, end), nil
		}
		out := map[string]any{"start_date": start, "end_date": end, "days_requested": days, "intake": map[string]any{"mean_calories_per_day": meanIn, "days_included": countIn, "days_excluded_unlogged": days - countIn, "low_item_count_dates": low}, "expenditure_garmin": map[string]any{"mean_total_calories_per_day": meanEx, "days_included": countEx, "days_excluded": days - countEx}}
		if len(readings) == 0 {
			out["body_composition_note"] = "No body-composition readings with both weight and body fat % in range."
			return out, nil
		}
		if len(readings) == 1 {
			out["body_composition_note"] = fmt.Sprintf("Only 1 qualifying body-composition reading (weight + body fat %%) in range, on %s. Need at least 2 to fit a trend.", readings[0].date)
			return out, nil
		}
		first, last := readings[0], readings[len(readings)-1]
		zero, _ := time.Parse("2006-01-02", first.date)
		xs, weights, fats, deficits := []float64{}, []float64{}, []float64{}, []float64{}
		for _, r := range readings {
			d, _ := time.Parse("2006-01-02", r.date)
			x := d.Sub(zero).Hours() / 24
			fat := r.weight * r.fat / 100
			xs = append(xs, x)
			weights = append(weights, r.weight)
			fats = append(fats, fat)
			deficits = append(deficits, -(fat*7700 + (r.weight-fat)*1800))
		}
		span := xs[len(xs)-1]
		bc := map[string]any{"readings_used": len(readings), "first_reading_date": first.date, "last_reading_date": last.date, "days_between_readings": span, "observed_first_weight_kg": rounded(first.weight, 100), "observed_last_weight_kg": rounded(last.weight, 100), "observed_first_body_fat_percent": first.fat, "observed_last_body_fat_percent": last.fat}
		out["body_composition"] = bc
		weightFit, fatFit := fitLine(xs, weights), fitLine(xs, fats)
		if weightFit == nil || fatFit == nil || span < 1 {
			out["derived_note"] = "Body composition readings found but a trend could not be fit (readings span zero days)."
			return out, nil
		}
		weightChange := weightFit.slope * span
		fatChange := fatFit.slope * span
		bc["fitted_first_weight_kg"] = rounded(weightFit.intercept, 100)
		bc["fitted_last_weight_kg"] = rounded(weightFit.intercept+weightChange, 100)
		bc["weight_change_kg"] = rounded(weightChange, 100)
		bc["fat_mass_change_kg"] = rounded(fatChange, 1000)
		bc["lean_mass_change_kg"] = rounded(weightChange-fatChange, 1000)
		if gate := bodyCompGate(xs); gate != "" {
			out["gate_note"] = "Derived TDEE suppressed: " + gate
			return out, nil
		}
		if countIn == 0 {
			out["derived_note"] = "Body composition trend fit but no qualifying intake days to derive a TDEE from."
			return out, nil
		}
		fit := fitLine(xs, deficits)
		tdee := sumIn/float64(countIn) + fit.slope
		derived := map[string]any{"implied_deficit_kcal_per_day": rounded(fit.slope, 10), "implied_deficit_uncertainty_kcal_per_day": nil, "composition_derived_tdee_kcal_per_day": rounded(tdee, 10), "garmin_tdee_kcal_per_day": meanEx, "difference_kcal_per_day": nil, "note": "composition_derived_tdee is a model estimate, not ground truth. difference_kcal_per_day is not attributable to Garmin's model or to under-logged intake without independent validation of either. A window with a high days_excluded_unlogged count biases this estimate downward, since under-logged days pull down the intake mean it's built from."}
		if fit.se != nil {
			derived["implied_deficit_uncertainty_kcal_per_day"] = rounded(fit.se, 10)
		} else {
			derived["uncertainty_note"] = "Only 2 readings -- no residual variance to estimate uncertainty from. More readings in range would let this be quantified."
		}
		if countEx > 0 {
			derived["difference_kcal_per_day"] = rounded(sumEx/float64(countEx)-tdee, 10)
		}
		out["derived"] = derived
		out["assumptions"] = map[string]any{"fat_kcal_per_kg": 7700, "lean_mass_kcal_per_kg": 1800, "note": "Standard energy-density approximations from body-composition literature, not Garmin-provided or measured constants. Fat-free mass change over short windows is often water/glycogen rather than structural tissue, which carries little caloric weight -- this biases lean_mass_kcal_per_kg high for short windows."}
		return out, nil
	}, "start_date", "end_date")
}
