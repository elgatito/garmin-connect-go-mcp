package server

// Tool descriptions: the Python docstrings without their Args sections (the argument text is in the input structs).

const (
	progressSummaryDesc = `Get progress summary for a metric between dates

Note: "calories" values are returned in kcal. Garmin's underlying
endpoint stores this metric pre-scaled by ~4.19; this tool undoes
that scaling before returning.`

	trainingEffectDesc = `Get training effect data for a specific activity.

Uses activity details first. If Garmin returns HTTP 403 for that
endpoint (seen on valid, recent activities), falls back to activity
list search, which still includes aerobic/anaerobic training effect.`

	trainingStatusDesc = `Get training status with curated metrics

Returns comprehensive training status including load, VO2 max, recovery,
and training readiness indicators.`

	cyclingFTPDesc = `Get the latest cycling Functional Threshold Power (FTP) data.

Returns the most recent cycling FTP estimate available from Garmin.`

	lactateThresholdDesc = `Get lactate threshold data

Returns lactate threshold information, which is the exercise intensity at
which lactate starts to accumulate in the blood. This is a key metric for
endurance training.`

	trainingLoadTrendDesc = `Get Garmin's daily acute and chronic training load over a date range.

Returns, per day, Garmin's acute load (` + "`atl`" + `, the 7-day load), chronic load (` + "`ctl`" + `), their
difference (` + "`tsb`" + ` = ctl - atl), and Garmin's Acute:Chronic Workload Ratio (` + "`acwr`" + `,
` + "`acwr_status`" + `). These are Garmin's dailyTrainingLoadAcute/Chronic values, not TrainingPeaks
CTL/ATL: they are on Garmin's weekly load scale, so ` + "`tsb`" + ` is not comparable to TrainingPeaks
TSB and TSB form thresholds do not apply. Use ` + "`acwr`/`acwr_status`" + ` (and the optimal chronic
load range) to judge whether load is building steadily or spiking.

Recommended range: 4-8 weeks. Maximum: 90 days.
Empty days are skipped; a failed daily request returns an error.`

	trainingLoadBalanceDesc = `Get Garmin's Load Focus — the distribution of the trailing-month
training load across Aerobic Low, Aerobic High, and Anaerobic intensity
bands, plus the system's feedback phrase (e.g. AEROBIC_HIGH_SHORTAGE,
BALANCED, ANAEROBIC_SHORTAGE).

Use this to assess whether the athlete's training mix is balanced or
deficient in a particular intensity band. Each band reports its load
alongside Garmin's target range; a ` + "`status`" + ` of "below", "within", or
"above" is computed from the load relative to that range.

Garmin reports Load Focus per device. The primary activity tracker's
record is used when it can be identified; ` + "`load_device_id`" + ` and
` + "`load_device_name`" + ` say which device the numbers came from.`

	hrvTrendDesc = `Get HRV (Heart Rate Variability) trend over a date range.

Returns daily HRV values and weekly rolling averages. Single-day HRV is too noisy
to act on — use this tool to identify baseline shifts that signal accumulated fatigue
or recovery. A drop of >10ms from the 7-day baseline warrants reducing training load.

Recommended range: 7-21 days. Maximum: 30 days.
Empty days are skipped; a failed daily request returns an error.`

	vo2maxTrendDesc = `Get VO2 max trend over a date range.

Returns daily VO2 max estimates from Garmin's FirstBeat algorithm. Use this to track
whether training is producing fitness gains over weeks or months. Flat or declining
VO2 max over 4+ weeks suggests insufficient training stimulus or overreaching.

Note: VO2 max estimates are smoothed and update gradually — daily changes of <0.5
are within normal noise. Focus on the 4-6 week trend direction.

Garmin records a new VO2 max value only on days with a recompute (after an
activity). Days in between carry the last known value forward and are marked
with "carried_forward": true, matching the trend chart in Garmin Connect.
Dated status values retain their measurement date. data_points counts
displayed days, including carried-forward values.

If historical values are unavailable, the current profile estimate is returned
separately and is not represented as a historical trend point.
Endpoint-specific failures remain visible in request_errors when a
fallback succeeds. Authentication, rate-limit, and connection failures
stop the request.

Recommended range: 4-12 weeks. Maximum: 90 days.`

	respirationTrendDesc = `Get overnight respiration rate trend over a date range.

Elevated resting respiration rate (compared to personal baseline) is an early
warning sign for overreaching, illness, or poor recovery. Use this alongside HRV
trend for a complete recovery picture.

Recommended range: 7-21 days. Maximum: 30 days.
Empty days are skipped; a failed daily request returns an error.`

	runningToleranceDesc = `Get Running Tolerance for a single day.

Returns Garmin's running load capacity model: how much running load the
athlete can currently absorb (tolerance), the intensity-adjusted load
their recent runs have produced (acute load), and the raw distance behind
that load. All three are expressed in km so they're directly comparable —
` + "`load_ratio`" + ` (acute_load_km / distance_km) quantifies how much intensity
is inflating the cost of each kilometer run.`

	runningToleranceTrendDesc = `Get Running Tolerance trend over a date range.

Running Tolerance moves slowly — its value is in the trajectory, not any
single day. Returns, per period: tolerance_km (current load capacity),
acute_load_km (intensity-adjusted load), distance_km (actual distance
run), and load_ratio (acute_load_km / distance_km — how much intensity
inflates the cost of each kilometer). Weekly aggregation (default) gives
a compact multi-month view; daily gives day-to-day resolution for a
shorter window.

Recommended range: 4-12 weeks. Maximum: 90 days for daily aggregation,
366 days for weekly (this endpoint returns the whole range in one call,
so the limit protects output size, not request volume).`

	acclimationDesc = `Get heat and altitude acclimation status for a given date.

Garmin tracks how adapted the athlete currently is to training in heat
and at altitude. heat_acclimation_percent runs 0-100 and decays without
continued exposure; use it to judge readiness for a warm-weather race.

heat_trend reports Garmin's own label (e.g. ACCLIMATIZED). The
previous_* fields hold the prior reading so direction of travel is
visible without a second call.

VO2 max is not returned here; use get_training_status or get_vo2max_trend.`
)
