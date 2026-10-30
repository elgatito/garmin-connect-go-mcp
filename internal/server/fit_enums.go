// Code generated from the pinned FIT profile. DO NOT EDIT.
package server

import (
	"github.com/muktihari/fit/profile"
	"github.com/muktihari/fit/profile/typedef"
	"strings"
)

func fitEnumLabel(kind profile.ProfileType, value any) (any, bool) {
	if values, ok := value.([]any); ok {
		out := make([]any, len(values))
		found := false
		for i, v := range values {
			out[i], ok = fitEnumLabel(kind, v)
			if ok {
				found = true
			}
		}
		return out, found
	}
	n, ok := fitNumber(value)
	if !ok {
		return nil, false
	}
	var label string
	switch kind {
	case profile.ActivityClass:
		label = typedef.ActivityClass(n).String()
	case profile.Activity:
		label = typedef.Activity(n).String()
	case profile.ActivityLevel:
		label = typedef.ActivityLevel(n).String()
	case profile.ActivitySubtype:
		label = typedef.ActivitySubtype(n).String()
	case profile.ActivityType:
		label = typedef.ActivityType(n).String()
	case profile.AnalogWatchfaceLayout:
		label = typedef.AnalogWatchfaceLayout(n).String()
	case profile.AntNetwork:
		label = typedef.AntNetwork(n).String()
	case profile.AntplusDeviceType:
		label = typedef.AntplusDeviceType(n).String()
	case profile.ApneaDiscipline:
		label = typedef.ApneaDiscipline(n).String()
	case profile.AttitudeStage:
		label = typedef.AttitudeStage(n).String()
	case profile.AttitudeValidity:
		label = typedef.AttitudeValidity(n).String()
	case profile.AutoActivityDetect:
		label = typedef.AutoActivityDetect(n).String()
	case profile.AutoSyncFrequency:
		label = typedef.AutoSyncFrequency(n).String()
	case profile.AutolapTrigger:
		label = typedef.AutolapTrigger(n).String()
	case profile.Autoscroll:
		label = typedef.Autoscroll(n).String()
	case profile.BacklightMode:
		label = typedef.BacklightMode(n).String()
	case profile.BacklightTimeout:
		label = typedef.BacklightTimeout(n).String()
	case profile.BandedExercisesExerciseName:
		label = typedef.BandedExercisesExerciseName(n).String()
	case profile.BatteryStatus:
		label = typedef.BatteryStatus(n).String()
	case profile.BattleRopeExerciseName:
		label = typedef.BattleRopeExerciseName(n).String()
	case profile.BenchPressExerciseName:
		label = typedef.BenchPressExerciseName(n).String()
	case profile.BikeExerciseName:
		label = typedef.BikeExerciseName(n).String()
	case profile.BikeLightBeamAngleMode:
		label = typedef.BikeLightBeamAngleMode(n).String()
	case profile.BikeLightNetworkConfigType:
		label = typedef.BikeLightNetworkConfigType(n).String()
	case profile.BikeOutdoorExerciseName:
		label = typedef.BikeOutdoorExerciseName(n).String()
	case profile.BleDeviceType:
		label = typedef.BleDeviceType(n).String()
	case profile.BodyLocation:
		label = typedef.BodyLocation(n).String()
	case profile.BpStatus:
		label = typedef.BpStatus(n).String()
	case profile.BrazilianGradingScale:
		label = typedef.BrazilianGradingScale(n).String()
	case profile.BritishAdjectivalGradingScale:
		label = typedef.BritishAdjectivalGradingScale(n).String()
	case profile.BritishTechnicalGradingScale:
		label = typedef.BritishTechnicalGradingScale(n).String()
	case profile.CalfRaiseExerciseName:
		label = typedef.CalfRaiseExerciseName(n).String()
	case profile.CameraEventType:
		label = typedef.CameraEventType(n).String()
	case profile.CameraOrientationType:
		label = typedef.CameraOrientationType(n).String()
	case profile.CardioExerciseName:
		label = typedef.CardioExerciseName(n).String()
	case profile.CarryExerciseName:
		label = typedef.CarryExerciseName(n).String()
	case profile.CcrSetpointSwitchMode:
		label = typedef.CcrSetpointSwitchMode(n).String()
	case profile.Checksum:
		label = typedef.Checksum(n).String()
	case profile.ChopExerciseName:
		label = typedef.ChopExerciseName(n).String()
	case profile.ClimbGradingScale:
		label = typedef.ClimbGradingScale(n).String()
	case profile.ClimbProEvent:
		label = typedef.ClimbProEvent(n).String()
	case profile.CommTimeoutType:
		label = typedef.CommTimeoutType(n).String()
	case profile.CoreExerciseName:
		label = typedef.CoreExerciseName(n).String()
	case profile.CoursePoint:
		label = typedef.CoursePoint(n).String()
	case profile.CrunchExerciseName:
		label = typedef.CrunchExerciseName(n).String()
	case profile.CurlExerciseName:
		label = typedef.CurlExerciseName(n).String()
	case profile.DankyuGradingScale:
		label = typedef.DankyuGradingScale(n).String()
	case profile.DateMode:
		label = typedef.DateMode(n).String()
	case profile.DateTime:
		label = typedef.DateTime(n).String()
	case profile.DayOfWeek:
		label = typedef.DayOfWeek(n).String()
	case profile.DeadliftExerciseName:
		label = typedef.DeadliftExerciseName(n).String()
	case profile.DeviceIndex:
		label = typedef.DeviceIndex(n).String()
	case profile.DigitalWatchfaceLayout:
		label = typedef.DigitalWatchfaceLayout(n).String()
	case profile.DisplayHeart:
		label = typedef.DisplayHeart(n).String()
	case profile.DisplayMeasure:
		label = typedef.DisplayMeasure(n).String()
	case profile.DisplayOrientation:
		label = typedef.DisplayOrientation(n).String()
	case profile.DisplayPosition:
		label = typedef.DisplayPosition(n).String()
	case profile.DisplayPower:
		label = typedef.DisplayPower(n).String()
	case profile.DiveAlarmType:
		label = typedef.DiveAlarmType(n).String()
	case profile.DiveAlert:
		label = typedef.DiveAlert(n).String()
	case profile.DiveBacklightMode:
		label = typedef.DiveBacklightMode(n).String()
	case profile.DiveGasMode:
		label = typedef.DiveGasMode(n).String()
	case profile.DiveGasStatus:
		label = typedef.DiveGasStatus(n).String()
	case profile.DiveSectionType:
		label = typedef.DiveSectionType(n).String()
	case profile.EllipticalExerciseName:
		label = typedef.EllipticalExerciseName(n).String()
	case profile.Event:
		label = typedef.Event(n).String()
	case profile.EventType:
		label = typedef.EventType(n).String()
	case profile.EwbankGradingScale:
		label = typedef.EwbankGradingScale(n).String()
	case profile.ExdDataUnits:
		label = typedef.ExdDataUnits(n).String()
	case profile.ExdDescriptors:
		label = typedef.ExdDescriptors(n).String()
	case profile.ExdDisplayType:
		label = typedef.ExdDisplayType(n).String()
	case profile.ExdLayout:
		label = typedef.ExdLayout(n).String()
	case profile.ExdQualifiers:
		label = typedef.ExdQualifiers(n).String()
	case profile.ExerciseCategory:
		label = typedef.ExerciseCategory(n).String()
	case profile.FaveroProduct:
		label = typedef.FaveroProduct(n).String()
	case profile.File:
		label = typedef.File(n).String()
	case profile.FitBaseUnit:
		label = typedef.FitBaseUnit(n).String()
	case profile.FitnessEquipmentState:
		label = typedef.FitnessEquipmentState(n).String()
	case profile.FloorClimbExerciseName:
		label = typedef.FloorClimbExerciseName(n).String()
	case profile.FlyeExerciseName:
		label = typedef.FlyeExerciseName(n).String()
	case profile.FontGradingScale:
		label = typedef.FontGradingScale(n).String()
	case profile.FrenchGradingScale:
		label = typedef.FrenchGradingScale(n).String()
	case profile.GarminProduct:
		label = typedef.GarminProduct(n).String()
	case profile.GasConsumptionRateType:
		label = typedef.GasConsumptionRateType(n).String()
	case profile.Gender:
		label = typedef.Gender(n).String()
	case profile.Goal:
		label = typedef.Goal(n).String()
	case profile.GoalRecurrence:
		label = typedef.GoalRecurrence(n).String()
	case profile.GoalSource:
		label = typedef.GoalSource(n).String()
	case profile.HipRaiseExerciseName:
		label = typedef.HipRaiseExerciseName(n).String()
	case profile.HipStabilityExerciseName:
		label = typedef.HipStabilityExerciseName(n).String()
	case profile.HipSwingExerciseName:
		label = typedef.HipSwingExerciseName(n).String()
	case profile.HrType:
		label = typedef.HrType(n).String()
	case profile.HrZoneCalc:
		label = typedef.HrZoneCalc(n).String()
	case profile.HrvStatus:
		label = typedef.HrvStatus(n).String()
	case profile.HyperextensionExerciseName:
		label = typedef.HyperextensionExerciseName(n).String()
	case profile.IndoorBikeExerciseName:
		label = typedef.IndoorBikeExerciseName(n).String()
	case profile.IndoorRowExerciseName:
		label = typedef.IndoorRowExerciseName(n).String()
	case profile.Intensity:
		label = typedef.Intensity(n).String()
	case profile.LadderExerciseName:
		label = typedef.LadderExerciseName(n).String()
	case profile.Language:
		label = typedef.Language(n).String()
	case profile.LapTrigger:
		label = typedef.LapTrigger(n).String()
	case profile.LateralRaiseExerciseName:
		label = typedef.LateralRaiseExerciseName(n).String()
	case profile.LeftRightBalance100:
		label = typedef.LeftRightBalance100(n).String()
	case profile.LeftRightBalance:
		label = typedef.LeftRightBalance(n).String()
	case profile.LegCurlExerciseName:
		label = typedef.LegCurlExerciseName(n).String()
	case profile.LegRaiseExerciseName:
		label = typedef.LegRaiseExerciseName(n).String()
	case profile.LengthType:
		label = typedef.LengthType(n).String()
	case profile.LocalDateTime:
		label = typedef.LocalDateTime(n).String()
	case profile.LocalDeviceType:
		label = typedef.LocalDeviceType(n).String()
	case profile.LocaltimeIntoDay:
		label = typedef.LocaltimeIntoDay(n).String()
	case profile.LungeExerciseName:
		label = typedef.LungeExerciseName(n).String()
	case profile.Manufacturer:
		label = typedef.Manufacturer(n).String()
	case profile.MaxMetCategory:
		label = typedef.MaxMetCategory(n).String()
	case profile.MaxMetHeartRateSource:
		label = typedef.MaxMetHeartRateSource(n).String()
	case profile.MaxMetSpeedSource:
		label = typedef.MaxMetSpeedSource(n).String()
	case profile.MesgCount:
		label = typedef.MesgCount(n).String()
	case profile.MesgNum:
		label = typedef.MesgNum(n).String()
	case profile.MessageIndex:
		label = typedef.MessageIndex(n).String()
	case profile.MoveExerciseName:
		label = typedef.MoveExerciseName(n).String()
	case profile.NapPeriodFeedback:
		label = typedef.NapPeriodFeedback(n).String()
	case profile.NapSource:
		label = typedef.NapSource(n).String()
	case profile.NoFlyTimeMode:
		label = typedef.NoFlyTimeMode(n).String()
	case profile.OlympicLiftExerciseName:
		label = typedef.OlympicLiftExerciseName(n).String()
	case profile.PlankExerciseName:
		label = typedef.PlankExerciseName(n).String()
	case profile.PlyoExerciseName:
		label = typedef.PlyoExerciseName(n).String()
	case profile.PoseExerciseName:
		label = typedef.PoseExerciseName(n).String()
	case profile.PowerPhaseType:
		label = typedef.PowerPhaseType(n).String()
	case profile.ProjectileType:
		label = typedef.ProjectileType(n).String()
	case profile.PullUpExerciseName:
		label = typedef.PullUpExerciseName(n).String()
	case profile.PushUpExerciseName:
		label = typedef.PushUpExerciseName(n).String()
	case profile.PwrZoneCalc:
		label = typedef.PwrZoneCalc(n).String()
	case profile.RadarThreatLevelType:
		label = typedef.RadarThreatLevelType(n).String()
	case profile.RiderPositionType:
		label = typedef.RiderPositionType(n).String()
	case profile.RowExerciseName:
		label = typedef.RowExerciseName(n).String()
	case profile.RunExerciseName:
		label = typedef.RunExerciseName(n).String()
	case profile.RunIndoorExerciseName:
		label = typedef.RunIndoorExerciseName(n).String()
	case profile.SandbagExerciseName:
		label = typedef.SandbagExerciseName(n).String()
	case profile.SaxonGradingScale:
		label = typedef.SaxonGradingScale(n).String()
	case profile.Schedule:
		label = typedef.Schedule(n).String()
	case profile.SegmentDeleteStatus:
		label = typedef.SegmentDeleteStatus(n).String()
	case profile.SegmentLapStatus:
		label = typedef.SegmentLapStatus(n).String()
	case profile.SegmentLeaderboardType:
		label = typedef.SegmentLeaderboardType(n).String()
	case profile.SegmentSelectionType:
		label = typedef.SegmentSelectionType(n).String()
	case profile.SensorType:
		label = typedef.SensorType(n).String()
	case profile.SessionTrigger:
		label = typedef.SessionTrigger(n).String()
	case profile.SetType:
		label = typedef.SetType(n).String()
	case profile.ShoulderPressExerciseName:
		label = typedef.ShoulderPressExerciseName(n).String()
	case profile.ShoulderStabilityExerciseName:
		label = typedef.ShoulderStabilityExerciseName(n).String()
	case profile.ShrugExerciseName:
		label = typedef.ShrugExerciseName(n).String()
	case profile.Side:
		label = typedef.Side(n).String()
	case profile.SitUpExerciseName:
		label = typedef.SitUpExerciseName(n).String()
	case profile.SledExerciseName:
		label = typedef.SledExerciseName(n).String()
	case profile.SledgeHammerExerciseName:
		label = typedef.SledgeHammerExerciseName(n).String()
	case profile.SleepDisruptionSeverity:
		label = typedef.SleepDisruptionSeverity(n).String()
	case profile.SleepLevel:
		label = typedef.SleepLevel(n).String()
	case profile.SourceType:
		label = typedef.SourceType(n).String()
	case profile.SplitStatus:
		label = typedef.SplitStatus(n).String()
	case profile.SplitType:
		label = typedef.SplitType(n).String()
	case profile.Spo2MeasurementType:
		label = typedef.Spo2MeasurementType(n).String()
	case profile.SportEvent:
		label = typedef.SportEvent(n).String()
	case profile.Sport:
		label = typedef.Sport(n).String()
	case profile.SquatExerciseName:
		label = typedef.SquatExerciseName(n).String()
	case profile.StairStepperExerciseName:
		label = typedef.StairStepperExerciseName(n).String()
	case profile.StrokeType:
		label = typedef.StrokeType(n).String()
	case profile.SubSport:
		label = typedef.SubSport(n).String()
	case profile.SuspensionExerciseName:
		label = typedef.SuspensionExerciseName(n).String()
	case profile.SwimStroke:
		label = typedef.SwimStroke(n).String()
	case profile.Switch:
		label = typedef.Switch(n).String()
	case profile.TapSensitivity:
		label = typedef.TapSensitivity(n).String()
	case profile.TimeIntoDay:
		label = typedef.TimeIntoDay(n).String()
	case profile.TimeMode:
		label = typedef.TimeMode(n).String()
	case profile.TimeZone:
		label = typedef.TimeZone(n).String()
	case profile.TimerTrigger:
		label = typedef.TimerTrigger(n).String()
	case profile.TireExerciseName:
		label = typedef.TireExerciseName(n).String()
	case profile.TissueModelType:
		label = typedef.TissueModelType(n).String()
	case profile.Tone:
		label = typedef.Tone(n).String()
	case profile.TotalBodyExerciseName:
		label = typedef.TotalBodyExerciseName(n).String()
	case profile.TricepsExtensionExerciseName:
		label = typedef.TricepsExtensionExerciseName(n).String()
	case profile.TurnType:
		label = typedef.TurnType(n).String()
	case profile.UiaaGradingScale:
		label = typedef.UiaaGradingScale(n).String()
	case profile.UserLocalId:
		label = typedef.UserLocalId(n).String()
	case profile.VerminGradingScale:
		label = typedef.VerminGradingScale(n).String()
	case profile.WarmUpExerciseName:
		label = typedef.WarmUpExerciseName(n).String()
	case profile.WatchfaceMode:
		label = typedef.WatchfaceMode(n).String()
	case profile.WaterType:
		label = typedef.WaterType(n).String()
	case profile.WeatherReport:
		label = typedef.WeatherReport(n).String()
	case profile.WeatherSevereType:
		label = typedef.WeatherSevereType(n).String()
	case profile.WeatherSeverity:
		label = typedef.WeatherSeverity(n).String()
	case profile.WeatherStatus:
		label = typedef.WeatherStatus(n).String()
	case profile.Weight:
		label = typedef.Weight(n).String()
	case profile.WktStepDuration:
		label = typedef.WktStepDuration(n).String()
	case profile.WktStepTarget:
		label = typedef.WktStepTarget(n).String()
	case profile.WorkoutEquipment:
		label = typedef.WorkoutEquipment(n).String()
	case profile.WorkoutHr:
		label = typedef.WorkoutHr(n).String()
	case profile.WorkoutPower:
		label = typedef.WorkoutPower(n).String()
	case profile.YdsGradingScale:
		label = typedef.YdsGradingScale(n).String()
	default:
		return nil, false
	}
	if label == "" || strings.Contains(label, "Invalid(") {
		return nil, false
	}
	return label, true
}

func fitExerciseType(selector string) (profile.ProfileType, bool) {
	switch selector {
	case strings.TrimSuffix(profile.BandedExercisesExerciseName.String(), "_exercise_name"):
		return profile.BandedExercisesExerciseName, true
	case strings.TrimSuffix(profile.BattleRopeExerciseName.String(), "_exercise_name"):
		return profile.BattleRopeExerciseName, true
	case strings.TrimSuffix(profile.BenchPressExerciseName.String(), "_exercise_name"):
		return profile.BenchPressExerciseName, true
	case strings.TrimSuffix(profile.BikeExerciseName.String(), "_exercise_name"):
		return profile.BikeExerciseName, true
	case strings.TrimSuffix(profile.BikeOutdoorExerciseName.String(), "_exercise_name"):
		return profile.BikeOutdoorExerciseName, true
	case strings.TrimSuffix(profile.CalfRaiseExerciseName.String(), "_exercise_name"):
		return profile.CalfRaiseExerciseName, true
	case strings.TrimSuffix(profile.CardioExerciseName.String(), "_exercise_name"):
		return profile.CardioExerciseName, true
	case strings.TrimSuffix(profile.CarryExerciseName.String(), "_exercise_name"):
		return profile.CarryExerciseName, true
	case strings.TrimSuffix(profile.ChopExerciseName.String(), "_exercise_name"):
		return profile.ChopExerciseName, true
	case strings.TrimSuffix(profile.CoreExerciseName.String(), "_exercise_name"):
		return profile.CoreExerciseName, true
	case strings.TrimSuffix(profile.CrunchExerciseName.String(), "_exercise_name"):
		return profile.CrunchExerciseName, true
	case strings.TrimSuffix(profile.CurlExerciseName.String(), "_exercise_name"):
		return profile.CurlExerciseName, true
	case strings.TrimSuffix(profile.DeadliftExerciseName.String(), "_exercise_name"):
		return profile.DeadliftExerciseName, true
	case strings.TrimSuffix(profile.EllipticalExerciseName.String(), "_exercise_name"):
		return profile.EllipticalExerciseName, true
	case strings.TrimSuffix(profile.FloorClimbExerciseName.String(), "_exercise_name"):
		return profile.FloorClimbExerciseName, true
	case strings.TrimSuffix(profile.FlyeExerciseName.String(), "_exercise_name"):
		return profile.FlyeExerciseName, true
	case strings.TrimSuffix(profile.HipRaiseExerciseName.String(), "_exercise_name"):
		return profile.HipRaiseExerciseName, true
	case strings.TrimSuffix(profile.HipStabilityExerciseName.String(), "_exercise_name"):
		return profile.HipStabilityExerciseName, true
	case strings.TrimSuffix(profile.HipSwingExerciseName.String(), "_exercise_name"):
		return profile.HipSwingExerciseName, true
	case strings.TrimSuffix(profile.HyperextensionExerciseName.String(), "_exercise_name"):
		return profile.HyperextensionExerciseName, true
	case strings.TrimSuffix(profile.IndoorBikeExerciseName.String(), "_exercise_name"):
		return profile.IndoorBikeExerciseName, true
	case strings.TrimSuffix(profile.IndoorRowExerciseName.String(), "_exercise_name"):
		return profile.IndoorRowExerciseName, true
	case strings.TrimSuffix(profile.LadderExerciseName.String(), "_exercise_name"):
		return profile.LadderExerciseName, true
	case strings.TrimSuffix(profile.LateralRaiseExerciseName.String(), "_exercise_name"):
		return profile.LateralRaiseExerciseName, true
	case strings.TrimSuffix(profile.LegCurlExerciseName.String(), "_exercise_name"):
		return profile.LegCurlExerciseName, true
	case strings.TrimSuffix(profile.LegRaiseExerciseName.String(), "_exercise_name"):
		return profile.LegRaiseExerciseName, true
	case strings.TrimSuffix(profile.LungeExerciseName.String(), "_exercise_name"):
		return profile.LungeExerciseName, true
	case strings.TrimSuffix(profile.MoveExerciseName.String(), "_exercise_name"):
		return profile.MoveExerciseName, true
	case strings.TrimSuffix(profile.OlympicLiftExerciseName.String(), "_exercise_name"):
		return profile.OlympicLiftExerciseName, true
	case strings.TrimSuffix(profile.PlankExerciseName.String(), "_exercise_name"):
		return profile.PlankExerciseName, true
	case strings.TrimSuffix(profile.PlyoExerciseName.String(), "_exercise_name"):
		return profile.PlyoExerciseName, true
	case strings.TrimSuffix(profile.PoseExerciseName.String(), "_exercise_name"):
		return profile.PoseExerciseName, true
	case strings.TrimSuffix(profile.PullUpExerciseName.String(), "_exercise_name"):
		return profile.PullUpExerciseName, true
	case strings.TrimSuffix(profile.PushUpExerciseName.String(), "_exercise_name"):
		return profile.PushUpExerciseName, true
	case strings.TrimSuffix(profile.RowExerciseName.String(), "_exercise_name"):
		return profile.RowExerciseName, true
	case strings.TrimSuffix(profile.RunExerciseName.String(), "_exercise_name"):
		return profile.RunExerciseName, true
	case strings.TrimSuffix(profile.RunIndoorExerciseName.String(), "_exercise_name"):
		return profile.RunIndoorExerciseName, true
	case strings.TrimSuffix(profile.SandbagExerciseName.String(), "_exercise_name"):
		return profile.SandbagExerciseName, true
	case strings.TrimSuffix(profile.ShoulderPressExerciseName.String(), "_exercise_name"):
		return profile.ShoulderPressExerciseName, true
	case strings.TrimSuffix(profile.ShoulderStabilityExerciseName.String(), "_exercise_name"):
		return profile.ShoulderStabilityExerciseName, true
	case strings.TrimSuffix(profile.ShrugExerciseName.String(), "_exercise_name"):
		return profile.ShrugExerciseName, true
	case strings.TrimSuffix(profile.SitUpExerciseName.String(), "_exercise_name"):
		return profile.SitUpExerciseName, true
	case strings.TrimSuffix(profile.SledExerciseName.String(), "_exercise_name"):
		return profile.SledExerciseName, true
	case strings.TrimSuffix(profile.SledgeHammerExerciseName.String(), "_exercise_name"):
		return profile.SledgeHammerExerciseName, true
	case strings.TrimSuffix(profile.SquatExerciseName.String(), "_exercise_name"):
		return profile.SquatExerciseName, true
	case strings.TrimSuffix(profile.StairStepperExerciseName.String(), "_exercise_name"):
		return profile.StairStepperExerciseName, true
	case strings.TrimSuffix(profile.SuspensionExerciseName.String(), "_exercise_name"):
		return profile.SuspensionExerciseName, true
	case strings.TrimSuffix(profile.TireExerciseName.String(), "_exercise_name"):
		return profile.TireExerciseName, true
	case strings.TrimSuffix(profile.TotalBodyExerciseName.String(), "_exercise_name"):
		return profile.TotalBodyExerciseName, true
	case strings.TrimSuffix(profile.TricepsExtensionExerciseName.String(), "_exercise_name"):
		return profile.TricepsExtensionExerciseName, true
	case strings.TrimSuffix(profile.WarmUpExerciseName.String(), "_exercise_name"):
		return profile.WarmUpExerciseName, true
	}
	return 0, false
}
