package oracleprobe

import "testing"

func TestComparePropagatesIndependentSurfaceIDCopies(t *testing.T) {
	value := "same"

	tests := []struct {
		name  string
		sf    *Result
		glade *Result
		want  ComparisonStatus
	}{
		{name: "pass", sf: &Result{HasValue: true, Value: &value, ValueType: "String"}, glade: &Result{HasValue: true, Value: &value, ValueType: "String"}, want: StatusPass},
		{name: "fail", sf: &Result{HasValue: true, Value: &value, ValueType: "String"}, glade: &Result{ExceptionType: "System.Exception"}, want: StatusFail},
		{name: "inconclusive", sf: &Result{HasValue: true, Value: &value, ValueType: "String"}, want: StatusInconclusive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Case{ID: "linked", ValueType: "String", SurfaceIDs: []string{"apex:System.Linked.one()", "apex:System.Linked.two()"}}
			comparison := Compare(tt.sf, tt.glade, c)
			if comparison.Status != tt.want {
				t.Fatalf("status = %s, want %s", comparison.Status, tt.want)
			}
			if len(comparison.SurfaceIDs) != len(c.SurfaceIDs) {
				t.Fatalf("surface IDs = %#v, want %#v", comparison.SurfaceIDs, c.SurfaceIDs)
			}
			c.SurfaceIDs[0] = "changed-after-compare-" + tt.name
			if comparison.SurfaceIDs[0] == c.SurfaceIDs[0] {
				t.Fatal("comparison surface IDs alias the case slice")
			}
		})
	}
}
