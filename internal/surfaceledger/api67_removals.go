package surfaceledger

import "strings"

// These members remain in some docs/tooling snapshots for legacy compatibility
// but are rejected by the current Salesforce API. They are negative compiler
// contracts, not positive current-base surface rows.
var api67RemovedSurfaceKeys = map[string]struct{}{
	"apex:system.system.debug(object,object)":                                           {},
	"apex:system.site.getcurrentsiteurl":                                                {},
	"apex:system.site.getcustomwebaddress":                                              {},
	"apex:system.site.getprefix":                                                        {},
	"apex:system.database.insertasync(object,database.allowcallouts,accesslevel)":       {},
	"apex:system.database.insertasync(list<object>,database.allowcallouts,accesslevel)": {},
	"apex:system.database.updateasync(object,database.allowcallouts,accesslevel)":       {},
	"apex:system.database.updateasync(list<object>,database.allowcallouts,accesslevel)": {},
	"apex:system.database.deleteasync(object,database.allowcallouts,accesslevel)":       {},
	"apex:system.database.deleteasync(list<object>,database.allowcallouts,accesslevel)": {},
	// Salesforce documents the value-size statistics as available only through
	// API 49. Cache.Org.isAvailable is absent from the reviewed current docs.
	"apex:cache.org.getavgvaluesize":              {},
	"apex:cache.org.getmaxvaluesize":              {},
	"apex:cache.org.isavailable":                  {},
	"apex:cache.orgpartition.getavgvaluesize":     {},
	"apex:cache.orgpartition.getmaxvaluesize":     {},
	"apex:cache.partition.getavgvaluesize":        {},
	"apex:cache.partition.getmaxvaluesize":        {},
	"apex:cache.session.getavgvaluesize":          {},
	"apex:cache.session.getmaxvaluesize":          {},
	"apex:cache.sessionpartition.getavgvaluesize": {},
	"apex:cache.sessionpartition.getmaxvaluesize": {},
	// API-67 rejects the legacy top-level DeleteFilter alias. The supported
	// enums are Database.Cursor.DeleteFilter and
	// Database.PaginationCursor.DeleteFilter.
	"apex:database.deletefilter":                         {},
	"apex:database.deletefilter.deleted_rows_only":       {},
	"apex:database.deletefilter.no_deleted_rows":         {},
	"apex:database.deletefilter.no_deleted_sharing_rows": {},
	"apex:database.deletefilter.no_filter":               {},
	"apex:database.deletefilter.equals(object)":          {},
	"apex:database.deletefilter.hashcode":                {},
	"apex:database.deletefilter.ordinal":                 {},
	"apex:database.deletefilter.valueof(string)":         {},
	"apex:database.deletefilter.values":                  {},
	// The API-67 enum member is InProgress; IN_PROGRESS is a stale alias.
	"apex:metadata.deploystatus.in_progress": {},
}

func isAPI67RemovedSurfaceID(id string) bool {
	key := surfaceIDKey(id)
	key = strings.TrimSuffix(key, "()")
	_, removed := api67RemovedSurfaceKeys[key]
	return removed
}
