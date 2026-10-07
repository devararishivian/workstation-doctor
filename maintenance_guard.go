package main

import "errors"

// This user-facing guard stays active until confirmed, durable action execution
// replaces both legacy maintenance paths in Plan 5.
const legacyMaintenanceMessage = "Automatic maintenance is unavailable during the architecture migration."

var errLegacyMaintenanceUnavailable = errors.New("automatic maintenance is unavailable during the architecture migration")

func legacyMaintenanceError() error {
	return errLegacyMaintenanceUnavailable
}
