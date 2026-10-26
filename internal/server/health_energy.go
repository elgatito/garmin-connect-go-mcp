package server

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of get_energy_balance and its helpers (health_wellness.py).

const (
	energyMaxDays        = 61
	fatKcalPerKg         = 7700
	leanMassKcalPerKg    = 1800
	lowItemCountLimit    = 3
	statsChunkDays       = 28
	energyBalanceSummary = `Estimate a composition-derived TDEE from logged intake, Garmin's
claimed expenditure, and body-composition change over a date range.

Combines three signals Garmin exposes separately -- mean logged intake
(nutrition log), mean claimed expenditure (Garmin's total_calories from
daily stats), and a body-composition trend (weight and body-fat % from
smart-scale readings) -- into composition_derived_tdee, an estimate
independent of Garmin's calorie model:
composition_derived_tdee = mean_intake + implied_deficit, where
implied_deficit comes from the fitted rate of fat and fat-free mass
change, using the standard energy-density approximations
7700 kcal/kg fat and 1800 kcal/kg fat-free mass. These are body
composition literature estimates, not Garmin-provided or measured
values -- treat the result as a rough estimate, not a lab measurement.
difference_kcal_per_day (Garmin's claim minus this estimate) has at
least two explanations -- Garmin overestimating expenditure, or
under-logged intake, which is common -- and this tool cannot
distinguish between them.

The body-composition trend is fit by least-squares regression across
every qualifying scale reading in range, not just the first and last --
an endpoint-to-endpoint difference is fully determined by two
individual measurements and inherits all of their noise (a 0.5%
body-fat wobble, well within normal scale repeatability, can swing a
two-point estimate by over a hundred kcal/day). weight_change_kg,
fat_mass_change_kg, and lean_mass_change_kg in body_composition are
all fitted values (the fitted line's value at the last reading's day
offset minus its value at the first), not raw differences between the
observed_first_*/observed_last_* fields, which are the actual
readings for reference/sanity-checking, not what the change figures
are computed from -- fitted_first_weight_kg/fitted_last_weight_kg are
also included so fitted_last_weight_kg - fitted_first_weight_kg can be
checked against weight_change_kg directly.

implied_deficit_kcal_per_day is the slope of a single regression: for
each reading, compute -(fat_kg * 7700 + lean_kg * 1800) where
lean_kg = weight_kg - fat_kg, then fit that value against day offset.
This is mathematically identical to combining separately-fit fat and
lean slopes, but its standard error -- implied_deficit_uncertainty_kcal_per_day,
returned whenever more than 2 readings are used -- is that single
fit's own residual standard error, which is well-defined and
reproducible from the raw readings alone (fat_kg and lean_kg are both
derived from the same weight/body-fat pair per reading, so treating
their separately-fit slopes' errors as independent and combining them
after the fact, instead of fitting the combined series directly,
would not be reproducible from outside without also knowing that
assumption). Treat a deficit within roughly one standard error of
zero as indistinguishable from no change.

Below a minimum span, or when most of the fit's statistical leverage
sits in one tight cluster of readings taken within about 48 hours of
each other, the fitted trend is not reliable even with 3+ readings --
a close, low-noise pair inside that cluster makes the fit look
precise while the slope is still mostly an extrapolation from that
cluster to wherever the other reading(s) fall. When this gate fails,
body_composition is still returned (it's just the fitted line, always
checkable against the observed readings) but derived is omitted and
gate_note explains which condition failed. Prefer windows of 3+ weeks
with several readings spread across them, not clustered at one end.

Short windows are also dominated by water/glycogen shifts (which
carry little caloric weight, unlike true lean tissue), which biases
the fat-free-mass term; see assumptions.note.

Exclusions applied automatically, matching get_nutrition_summary_between_dates
and get_stats_range:
- Intake: days with no logged food (item_count 0) are excluded from
  the mean, not counted as zero. Days with a low but nonzero
  item_count remain included (excluding them requires judgment this
  tool doesn't make unilaterally) but are called out in
  low_item_count_dates so low-confidence days are visible rather than
  silently blended into the mean. A window with many such days (or
  many item_count-0 days) biases composition_derived_tdee downward --
  check days_excluded_unlogged and low_item_count_dates before
  comparing two windows against each other.
- Expenditure: days with no device data, and the current (partial)
  day, are excluded from the mean.
- Body composition: only scale readings with both weight and body-fat
  percentage are used (skips weight-only manual entries).

Needs at least 2 qualifying body-composition readings to derive a
trend, and to pass the span/leverage gate above to derive a TDEE from
it. With 0 or 1 readings, returns the intake/expenditure means and
whatever body composition data exists, but omits the derived TDEE.

Maximum range: 61 days per call.`
)

// dayValues keeps per-day entries in first-seen order so float sums match Python's dict iteration.
type dayValues[T any] struct {
	order []string
	m     map[string]T
}

func (d *dayValues[T]) set(date string, v T) {
	if d.m == nil {
		d.m = map[string]T{}
	}
	if _, ok := d.m[date]; !ok {
		d.order = append(d.order, date)
	}
	d.m[date] = v
}

type intakeDay struct {
	calories  any
	itemCount int
}

// fetchDailyIntake reads logged calories and item counts from the nutrition range endpoint.
func fetchDailyIntake(ctx context.Context, c *garmin.Client, start, end string) (*dayValues[intakeDay], error) {
	resp, err := c.GetJSON(ctx, "/nutrition-service/food/logs/range", url.Values{"startDate": {start}, "endDate": {end}})
	if err != nil {
		return nil, err
	}
	out := &dayValues[intakeDay]{}
	for _, d := range asList(asMap(resp)["dailyNutritionSummaries"]) {
		day := asMap(d)
		date, _ := day["mealDate"].(string)
		if date == "" {
			continue
		}
		count := 0
		for _, meal := range asList(day["mealDetails"]) {
			count += len(asList(asMap(meal)["loggedFoods"]))
		}
		out.set(date, intakeDay{asMap(day["dailyNutritionContent"])["calories"], count})
	}
	return out, nil
}

// fetchDailyCalories reads per-day calorie totals in chunks under Garmin's 28-day cap.
func fetchDailyCalories(ctx context.Context, c *garmin.Client, start, end time.Time) (*dayValues[map[string]any], error) {
	out := &dayValues[map[string]any]{}
	for cs := start; !cs.After(end); {
		ce := cs.AddDate(0, 0, statsChunkDays-1)
		if ce.After(end) {
			ce = end
		}
		path := "/usersummary-service/stats/daily/" + cs.Format(time.DateOnly) + "/" + ce.Format(time.DateOnly)
		resp, err := c.GetJSON(ctx, path, url.Values{"statsType": {"CALORIES"}})
		if err != nil {
			return nil, err
		}
		for _, e := range asList(asMap(resp)["values"]) {
			entry := asMap(e)
			if date, _ := entry["calendarDate"].(string); date != "" {
				out.set(date, asMap(entry["values"]))
			}
		}
		cs = ce.AddDate(0, 0, 1)
	}
	return out, nil
}

type linearFit struct {
	slope, intercept float64
	seSlope          *float64
}

// fitLine is an ordinary least-squares fit of ys against xs; nil when under two points or all xs equal.
// seSlope is the slope's standard error when there are more than 2 points.
func fitLine(xs, ys []float64) *linearFit {
	n := float64(len(xs))
	if n < 2 {
		return nil
	}
	var sumX, sumY float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX, meanY := sumX/n, sumY/n
	var sxx, sxy float64
	for i := range xs {
		sxx += (xs[i] - meanX) * (xs[i] - meanX)
		sxy += (xs[i] - meanX) * (ys[i] - meanY)
	}
	if sxx == 0 {
		return nil
	}
	f := &linearFit{slope: sxy / sxx}
	f.intercept = meanY - f.slope*meanX
	if n > 2 {
		var sse float64
		for i := range xs {
			r := ys[i] - (f.slope*xs[i] + f.intercept)
			sse += r * r
		}
		se := math.Sqrt(sse / (n - 2) / sxx)
		f.seSlope = &se
	}
	return f
}

// bodyCompGateFailure explains why readings (day offsets) are too poorly conditioned to fit a trend from;
// "" means they pass: they must span at least 21 days and no 48h cluster may hold most of the fit's leverage.
func bodyCompGateFailure(xs []float64) string {
	const minSpanDays = 21
	n := float64(len(xs))
	lo, hi := slicesMinMax(xs)
	if span := hi - lo; span < minSpanDays {
		return fmt.Sprintf("readings span only %s days; need at least %d to separate a real trend from water-weight noise",
			strconv.FormatFloat(span, 'f', -1, 64), minSpanDays)
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / n
	var sxx float64
	for _, x := range xs {
		sxx += (x - mean) * (x - mean)
	}
	if sxx == 0 {
		return "all readings fall on the same day"
	}
	leverages := make([]float64, len(xs))
	var total float64
	for i, x := range xs {
		leverages[i] = 1/n + (x-mean)*(x-mean)/sxx
		total += leverages[i]
	}
	// A cluster is every reading within 2 days of its first reading, tried from each reading in turn.
	var maxCluster float64
	for _, anchor := range xs {
		var c float64
		for i, x := range xs {
			if d := x - anchor; d >= 0 && d <= 2 {
				c += leverages[i]
			}
		}
		maxCluster = max(maxCluster, c)
	}
	if maxCluster > total/2 {
		return fmt.Sprintf("a single cluster of readings within 48h of each other accounts for %.0f%% of the fit's statistical "+
			"leverage -- the trend across the full window is not reliably distinguishable from noise in that cluster alone",
			maxCluster/total*100)
	}
	return ""
}

func slicesMinMax(xs []float64) (lo, hi float64) {
	lo, hi = xs[0], xs[0]
	for _, x := range xs {
		lo, hi = min(lo, x), max(hi, x)
	}
	return
}

type reading struct {
	date          string
	weight, bodyF float64
}

func getEnergyBalance(ctx context.Context, s *Session, startDate, endDate string) (any, error) {
	start, err := parseISODate(startDate)
	if err == nil {
		var end time.Time
		if end, err = parseISODate(endDate); err == nil {
			return energyBalanceRange(ctx, s, startDate, endDate, start, end)
		}
	}
	return fmt.Sprintf("Invalid date format: %s. Use YYYY-MM-DD.", err), nil
}

func energyBalanceRange(ctx context.Context, s *Session, startDate, endDate string, start, end time.Time) (any, error) {
	daysRequested := daysBetween(start, end) + 1
	if daysRequested < 1 {
		return "end_date must be on or after start_date.", nil
	}
	if daysRequested > energyMaxDays {
		return fmt.Sprintf("Date range too large (%d days). Maximum is %d days.", daysRequested, energyMaxDays), nil
	}

	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error retrieving energy balance data: %w", err) }
	intake, err := fetchDailyIntake(ctx, s.Client, startDate, endDate)
	if err != nil {
		return fail(err)
	}
	expenditure, err := fetchDailyCalories(ctx, s.Client, start, end)
	if err != nil {
		return fail(err)
	}
	bodyComp, err := s.Client.GetBodyComposition(ctx, startDate, endDate)
	if err != nil {
		return fail(err)
	}
	today := time.Now().Format(time.DateOnly)

	var loggedCalories []float64
	var lowItemDates []string
	for _, date := range intake.order {
		v := intake.m[date]
		if cal, ok := asNumber(v.calories); ok && v.itemCount > 0 {
			loggedCalories = append(loggedCalories, cal)
		}
		if v.itemCount > 0 && v.itemCount < lowItemCountLimit {
			lowItemDates = append(lowItemDates, date)
		}
	}
	sort.Strings(lowItemDates)
	if lowItemDates == nil {
		lowItemDates = []string{}
	}
	meanIntake := mean(loggedCalories)

	var expValues []float64
	for _, date := range expenditure.order {
		if total, ok := asNumber(expenditure.m[date]["totalCalories"]); ok && date != today {
			expValues = append(expValues, total)
		}
	}
	meanExpenditure := mean(expValues)

	// Only readings with both weight and body fat % are usable.
	var readings []reading
	for _, e := range asList(asMap(bodyComp)["dateWeightList"]) {
		entry := asMap(e)
		date, _ := entry["calendarDate"].(string)
		w, wOK := asNumber(entry["weight"])
		bf, bfOK := asNumber(entry["bodyFat"])
		if date != "" && wOK && bfOK {
			readings = append(readings, reading{date, w, bf})
		}
	}
	sort.SliceStable(readings, func(i, j int) bool { return readings[i].date < readings[j].date })

	result := map[string]any{
		"start_date":     startDate,
		"end_date":       endDate,
		"days_requested": daysRequested,
		"intake": map[string]any{
			"mean_calories_per_day":  roundPtr(meanIntake, 1),
			"days_included":          len(loggedCalories),
			"days_excluded_unlogged": daysRequested - len(loggedCalories),
			"low_item_count_dates":   lowItemDates,
		},
		"expenditure_garmin": map[string]any{
			"mean_total_calories_per_day": roundPtr(meanExpenditure, 1),
			"days_included":               len(expValues),
			"days_excluded":               daysRequested - len(expValues),
		},
	}

	switch {
	case len(readings) >= 2:
		addBodyCompositionTrend(result, readings, meanIntake, meanExpenditure)
	case len(readings) == 1:
		result["body_composition_note"] = fmt.Sprintf("Only 1 qualifying body-composition reading (weight + body fat %%) in range, "+
			"on %s. Need at least 2 to fit a trend.", readings[0].date)
	default:
		result["body_composition_note"] = "No body-composition readings with both weight and body fat % in range."
	}

	if len(loggedCalories) == 0 && len(expValues) == 0 && len(readings) == 0 {
		return fmt.Sprintf("No usable data found between %s and %s.", startDate, endDate), nil
	}
	return result, nil
}

// addBodyCompositionTrend adds body_composition and, when the gate passes, the derived TDEE (readings sorted, 2+).
func addBodyCompositionTrend(result map[string]any, readings []reading, meanIntake, meanExpenditure *float64) {
	firstDate, lastDate := readings[0].date, readings[len(readings)-1].date
	day0, _ := parseISODate(firstDate)
	xs := make([]float64, len(readings))
	weightKg := make([]float64, len(readings))
	fatKg := make([]float64, len(readings))
	for i, r := range readings {
		d, _ := parseISODate(r.date) // dates came from Garmin; an unparsable one reads as day 0
		xs[i] = float64(daysBetween(day0, d))
		weightKg[i] = r.weight / 1000
		fatKg[i] = weightKg[i] * r.bodyF / 100
	}
	spanDays := xs[len(xs)-1] - xs[0]
	weightFit, fatFit := fitLine(xs, weightKg), fitLine(xs, fatKg)

	bc := map[string]any{
		"readings_used":                   len(readings),
		"first_reading_date":              firstDate,
		"last_reading_date":               lastDate,
		"days_between_readings":           int(spanDays),
		"observed_first_weight_kg":        round(weightKg[0], 2),
		"observed_last_weight_kg":         round(weightKg[len(weightKg)-1], 2),
		"observed_first_body_fat_percent": readings[0].bodyF,
		"observed_last_body_fat_percent":  readings[len(readings)-1].bodyF,
	}
	result["body_composition"] = bc

	gate := bodyCompGateFailure(xs)
	if weightFit == nil || fatFit == nil || spanDays < 1 {
		result["derived_note"] = "Body composition readings found but a trend could not be fit (readings span zero days)."
		return
	}
	fatChange := fatFit.slope * spanDays
	weightChange := weightFit.slope * spanDays
	bc["fitted_first_weight_kg"] = round(weightFit.intercept+weightFit.slope*xs[0], 2)
	bc["fitted_last_weight_kg"] = round(weightFit.intercept+weightFit.slope*xs[len(xs)-1], 2)
	bc["weight_change_kg"] = round(weightChange, 2)
	bc["fat_mass_change_kg"] = round(fatChange, 3)
	bc["lean_mass_change_kg"] = round(weightChange-fatChange, 3)

	switch {
	case gate != "":
		result["gate_note"] = "Derived TDEE suppressed: " + gate
	case meanIntake == nil:
		result["derived_note"] = "Body composition trend fit but no qualifying intake days to derive a TDEE from."
	default:
		// One regression of the per-reading implied deficit against day offset, so its standard error is that
		// fit's own residual SE rather than a combination of two correlated fits' errors.
		deficit := make([]float64, len(readings))
		for i := range readings {
			deficit[i] = -(fatKg[i]*fatKcalPerKg + (weightKg[i]-fatKg[i])*leanMassKcalPerKg)
		}
		fit := fitLine(xs, deficit) // non-nil: the same xs gave weightFit
		tdee := *meanIntake + fit.slope
		derived := map[string]any{
			"implied_deficit_kcal_per_day":             round(fit.slope, 1),
			"implied_deficit_uncertainty_kcal_per_day": roundPtr(fit.seSlope, 1),
			"composition_derived_tdee_kcal_per_day":    round(tdee, 1),
			"garmin_tdee_kcal_per_day":                 roundPtr(meanExpenditure, 1),
			"difference_kcal_per_day":                  nil,
		}
		if meanExpenditure != nil {
			derived["difference_kcal_per_day"] = round(*meanExpenditure-tdee, 1)
		}
		if fit.seSlope == nil {
			derived["uncertainty_note"] = "Only 2 readings -- no residual variance to estimate uncertainty from. " +
				"More readings in range would let this be quantified."
		}
		derived["note"] = "composition_derived_tdee is a model estimate, not ground truth. " +
			"difference_kcal_per_day is not attributable to Garmin's model or to " +
			"under-logged intake without independent validation of either. A window " +
			"with a high days_excluded_unlogged count biases this estimate downward, " +
			"since under-logged days pull down the intake mean it's built from."
		result["derived"] = derived
		result["assumptions"] = map[string]any{
			"fat_kcal_per_kg":       fatKcalPerKg,
			"lean_mass_kcal_per_kg": leanMassKcalPerKg,
			"note": "Standard energy-density approximations from body-composition " +
				"literature, not Garmin-provided or measured constants. Fat-free " +
				"mass change over short windows is often water/glycogen rather than " +
				"structural tissue, which carries little caloric weight -- this " +
				"biases lean_mass_kcal_per_kg high for short windows.",
		}
	}
}

// mean is nil for no values.
func mean(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	m := sum / float64(len(xs))
	return &m
}

// roundPtr is round(x, digits) for an optional value (nil stays null).
func roundPtr(x *float64, digits int) any {
	if x == nil {
		return nil
	}
	return round(*x, digits)
}
