package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/sofar"
)

type snapshotReader func(context.Context, sofar.HardwareConfig) (domain.TelemetrySnapshot, error)

func main() {
	log.SetFlags(0)
	if err := run(os.Stdout, os.Getenv, readSnapshot); err != nil {
		log.Fatal(err)
	}
}

func run(output io.Writer, lookup func(string) string, read snapshotReader) error {
	if !sofar.HardwareTestEnabled(lookup) {
		return errors.New("hardware probe requires HELIO_HARDWARE_TEST=1")
	}
	config, err := sofar.HardwareConfigFromLookup(lookup)
	if err != nil {
		return err
	}
	snapshot, err := read(context.Background(), config)
	if err != nil {
		return errors.New("hardware probe failed")
	}
	encoded, err := sofar.MarshalHardwareSnapshot(snapshot)
	if err != nil {
		return errors.New("encode hardware snapshot failed")
	}
	if _, err := fmt.Fprintln(output, string(encoded)); err != nil {
		return errors.New("write hardware snapshot failed")
	}
	return nil
}

func readSnapshot(ctx context.Context, config sofar.HardwareConfig) (domain.TelemetrySnapshot, error) {
	return sofar.NewHardwareReader(config).ReadSnapshot(ctx)
}
