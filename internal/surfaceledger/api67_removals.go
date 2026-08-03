package surfaceledger

import "strings"

// These members remain in some docs/tooling snapshots for legacy compatibility
// but are rejected by the current Salesforce API. They are negative compiler
// contracts, not positive current-base surface rows.
var api67RemovedSurfaceKeys = map[string]struct{}{
	"apex:system.site.getcurrentsiteurl":    {},
	"apex:system.site.getcustomwebaddress":  {},
	"apex:system.site.getprefix":            {},
}

func isAPI67RemovedSurfaceID(id string) bool {
	key := surfaceIDKey(id)
	key = strings.TrimSuffix(key, "()")
	_, removed := api67RemovedSurfaceKeys[key]
	return removed
}
