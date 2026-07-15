// Package solar provides deterministic solar position calculations.
package solar

import (
	"errors"
	"math"
	"time"
)

var (
	ErrSunNeverRises = errors.New("sun never rises on this date")
	ErrSunNeverSets  = errors.New("sun never sets on this date")
)

const zenith = 90.833

// Daylight returns sunrise and sunset for the configured local calendar day.
func Daylight(date time.Time, latitude, longitude float64, location *time.Location) (time.Time, time.Time, error) {
	if location == nil {
		location = time.UTC
	}
	local := date.In(location)
	n := float64(local.YearDay())
	riseHours, err := solarEventUTC(n, latitude, longitude, true)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	setHours, err := solarEventUTC(n, latitude, longitude, false)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	utcMidnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	rise := alignLocalDay(utcMidnight.Add(time.Duration(riseHours*float64(time.Hour))), local, location)
	set := alignLocalDay(utcMidnight.Add(time.Duration(setHours*float64(time.Hour))), local, location)
	return rise, set, nil
}

func alignLocalDay(candidate, target time.Time, location *time.Location) time.Time {
	localCandidate := candidate.In(location)
	targetDay := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, location)
	candidateDay := time.Date(localCandidate.Year(), localCandidate.Month(), localCandidate.Day(), 0, 0, 0, 0, location)
	if candidateDay.Before(targetDay) {
		candidate = candidate.Add(24 * time.Hour)
	} else if candidateDay.After(targetDay) {
		candidate = candidate.Add(-24 * time.Hour)
	}
	return candidate.In(location)
}

func solarEventUTC(day, latitude, longitude float64, sunrise bool) (float64, error) {
	lngHour := longitude / 15
	targetHour := 18.0
	if sunrise {
		targetHour = 6
	}
	t := day + (targetHour-lngHour)/24
	meanAnomaly := 0.9856*t - 3.289
	longitudeSun := normalizeDegrees(meanAnomaly + 1.916*math.Sin(toRadians(meanAnomaly)) + 0.020*math.Sin(2*toRadians(meanAnomaly)) + 282.634)
	rightAscension := normalizeDegrees(toDegrees(math.Atan(0.91764 * math.Tan(toRadians(longitudeSun)))))
	rightAscension += math.Floor(longitudeSun/90)*90 - math.Floor(rightAscension/90)*90
	rightAscension /= 15
	sinDeclination := 0.39782 * math.Sin(toRadians(longitudeSun))
	cosDeclination := math.Cos(math.Asin(sinDeclination))
	cosHour := (math.Cos(toRadians(zenith)) - sinDeclination*math.Sin(toRadians(latitude))) / (cosDeclination * math.Cos(toRadians(latitude)))
	if cosHour > 1 {
		return 0, ErrSunNeverRises
	}
	if cosHour < -1 {
		return 0, ErrSunNeverSets
	}
	hourAngle := toDegrees(math.Acos(cosHour))
	if sunrise {
		hourAngle = 360 - hourAngle
	}
	hourAngle /= 15
	localMeanTime := hourAngle + rightAscension - 0.06571*t - 6.622
	return normalizeHours(localMeanTime - lngHour), nil
}

// Elevation returns the apparent solar elevation in degrees at an instant.
func Elevation(at time.Time, latitude, longitude float64) float64 {
	utc := at.UTC()
	days := 365.0
	if time.Date(utc.Year(), 12, 31, 0, 0, 0, 0, time.UTC).YearDay() == 366 {
		days = 366
	}
	hour := float64(utc.Hour()) + float64(utc.Minute())/60 + float64(utc.Second())/3600 + float64(utc.Nanosecond())/float64(time.Hour)
	gamma := 2 * math.Pi / days * (float64(utc.YearDay()-1) + (hour-12)/24)
	equationOfTime := 229.18 * (0.000075 + 0.001868*math.Cos(gamma) - 0.032077*math.Sin(gamma) - 0.014615*math.Cos(2*gamma) - 0.040849*math.Sin(2*gamma))
	declination := 0.006918 - 0.399912*math.Cos(gamma) + 0.070257*math.Sin(gamma) - 0.006758*math.Cos(2*gamma) + 0.000907*math.Sin(2*gamma) - 0.002697*math.Cos(3*gamma) + 0.00148*math.Sin(3*gamma)
	trueSolarMinutes := math.Mod(hour*60+equationOfTime+4*longitude, 1440)
	if trueSolarMinutes < 0 {
		trueSolarMinutes += 1440
	}
	hourAngle := toRadians(trueSolarMinutes/4 - 180)
	latitudeRad := toRadians(latitude)
	cosZenith := math.Sin(latitudeRad)*math.Sin(declination) + math.Cos(latitudeRad)*math.Cos(declination)*math.Cos(hourAngle)
	cosZenith = math.Max(-1, math.Min(1, cosZenith))
	return 90 - toDegrees(math.Acos(cosZenith))
}

func normalizeDegrees(value float64) float64 { return normalize(value, 360) }
func normalizeHours(value float64) float64   { return normalize(value, 24) }
func normalize(value, modulus float64) float64 {
	value = math.Mod(value, modulus)
	if value < 0 {
		value += modulus
	}
	return value
}
func toRadians(degrees float64) float64 { return degrees * math.Pi / 180 }
func toDegrees(radians float64) float64 { return radians * 180 / math.Pi }
