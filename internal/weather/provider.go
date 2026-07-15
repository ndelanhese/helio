// Package weather supplies optional, provider-neutral weather context.
package weather

import (
	"context"
	"time"
)

type Request struct {
	Latitude  float64
	Longitude float64
	Start     time.Time
	End       time.Time
}

type Hour struct {
	Time          time.Time
	CloudCoverPct float64
	IrradianceWM2 float64
	Source        string
	FetchedAt     time.Time
}

type Provider interface {
	Hourly(context.Context, Request) ([]Hour, error)
}
