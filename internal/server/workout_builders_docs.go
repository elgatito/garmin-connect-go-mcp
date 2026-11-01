package server

// Tool descriptions: the Python docstrings without their Args sections (the argument text is in the input structs).
const (
	createWalkRunDesc = `Create a walk/run interval workout and upload it to Garmin Connect.

Builds the internal Garmin JSON automatically and returns the new workout ID.`

	createRunDesc = `Create a continuous run workout and upload it to Garmin Connect.

Builds a single uninterrupted run interval with warmup and cooldown walks.

Targets a named Garmin heart-rate zone by default. Named zones (Z1-Z5)
don't line up with every real training target -- e.g. a 136-148 bpm
Zone 2 goal straddles Garmin's Z2 (118-137) and Z3 (138-157). Pass
hr_min and hr_max together to target that exact bpm range instead;
the watch will then show "in range" only for the range you actually
want, not a whole zone that over- or under-shoots it.`

	createRunIntervalDesc = `Create a repeat-based interval run workout and upload it to Garmin Connect.

Builds warmup -> repeats x [work interval + jog recovery] -> cooldown. Use this
instead of create_run_workout whenever the session has more than one work
interval (threshold reps, VO2 intervals, cruise intervals, strides).

Work intervals target a named heart-rate zone by default, but named zones are
usually far too wide for interval work: a threshold session at 172-180 bpm sits
inside a Z3 that may span 158-184. Pass hr_min and hr_max together to target the
exact bpm range, so the watch alerts as soon as the effort drifts off target.

Recovery steps carry no target unless recovery_hr_min/max are given -- a capped
recovery alerts constantly right after a hard rep, since heart rate lags effort.
Recovery HR bounds require recovery_seconds > 0.

Example -- 3 x 6 min at HR 172-180 with 3 min jogs, 12m warmup, 8m cooldown:
    repeats=3, rep_seconds=360, recovery_seconds=180,
    hr_min=172, hr_max=180, warmup_min=12, cooldown_min=8`

	createZ2WalkDesc = `Create a steady Z2 walking workout and upload it to Garmin Connect.`

	createStrengthDesc = `Create a strength workout and upload it to Garmin Connect.

Each exercise becomes a reps-based step; when sets > 1 the exercise is
emitted as a repeat group of that many iterations (work + rest per set),
so Garmin shows the real set count. The name is kept in the step
description; it is also sent as exerciseName, which Garmin only retains when
it matches one of its own exercise keys (e.g. "FARMERS_CARRY").`

	scheduleWeekDesc = `Schedule a list of workouts for the week in a single call.

Idempotent: if a workout is already scheduled for that date, it is
reported as already scheduled and the POST is skipped (avoids
duplicating calendar entries).

Each entry is handled on its own: an invalid or failing entry is
reported with status "failed" or "error" and the rest still run.
A successful entry includes scheduled_workout_id when Garmin returns
the calendar-entry ID; pass it to unschedule_workout to remove the entry.`
)
