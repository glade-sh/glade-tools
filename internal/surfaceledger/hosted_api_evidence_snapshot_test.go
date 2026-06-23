package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostedAPIUnsupportedEvidenceFixtures(t *testing.T) {
	fixtures := []struct {
		path    string
		count   int
		product string
		area    string
		samples []string
	}{
		{
			path:    "../../docs/fixtures/hosted-connect-rest-api-unsupported-surfaces.json",
			count:   7674,
			product: ProductConnectRESTAPI,
			area:    AreaServer,
			samples: []string{
				"connect-rest-api:CR_quickstart_oauth",
				"connect-rest-api:connect_requests_action_link_input",
				"connect-rest-api:quickstart_postman",
			},
		},
		{
			path:    "../../docs/fixtures/hosted-rest-resource-field-unsupported-surfaces.json",
			count:   65,
			product: ProductREST,
			area:    AreaServer,
			samples: []string{
				"rest:resources_composite_graph.graphid",
				"rest:resources_query_performance_feedback.relativecost",
				"rest:responses_ls_timeslots.remainingappointments",
			},
		},
		{
			path:    "../../docs/fixtures/hosted-tooling-run-retrieve-field-unsupported-surfaces.json",
			count:   25,
			product: ProductTooling,
			area:    AreaServer,
			samples: []string{
				"tooling:Retrieve.name",
				"tooling:Run.testLevel",
				"tooling:Run.stackTrace",
			},
		},
		{
			path:    "../../docs/fixtures/hosted-graphql-site-reference-unsupported-surfaces.json",
			count:   5,
			product: ProductSiteReferences,
			area:    AreaUI,
			samples: []string{
				"site-references:platform/graphql/graphql",
				"site-references:platform/graphql/index.Request",
				"site-references:platform/graphql/index.Responses",
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(filepath.Base(fixture.path), func(t *testing.T) {
			var raw struct {
				Evidence []struct {
					SurfaceID string `json:"surfaceId"`
				} `json:"evidence"`
			}
			data, err := os.ReadFile(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			if len(raw.Evidence) != fixture.count {
				t.Fatalf("evidence rows = %d, want %d", len(raw.Evidence), fixture.count)
			}
			for _, evidence := range raw.Evidence {
				if containsHiddenFormatMark(evidence.SurfaceID) {
					t.Fatalf("surface id contains hidden format mark: %q", evidence.SurfaceID)
				}
			}

			rows, err := BuildEvidenceSnapshot([]string{fixture.path})
			if err != nil {
				t.Fatal(err)
			}
			byID := rowsByID(rows)
			if len(byID) != fixture.count {
				t.Fatalf("snapshot rows = %d, want %d", len(byID), fixture.count)
			}
			for id, row := range byID {
				if row.Product != fixture.product || row.Area != fixture.area || row.Evidence != EvidenceFixture || row.GladeShape != ShapeAbsent || row.GladeBehavior != BehaviorUnsupported {
					t.Fatalf("%s state = product:%s area:%s evidence:%s shape:%s behavior:%s, want %s/%s/fixture/absent/unsupported", id, row.Product, row.Area, row.Evidence, row.GladeShape, row.GladeBehavior, fixture.product, fixture.area)
				}
			}
			for _, sample := range fixture.samples {
				row, ok := byID[sample]
				if !ok {
					t.Fatalf("missing sample row %s", sample)
				}
				if row.Evidence != EvidenceFixture || row.GladeShape != ShapeAbsent || row.GladeBehavior != BehaviorUnsupported {
					t.Fatalf("%s state = evidence:%s shape:%s behavior:%s, want fixture/absent/unsupported", sample, row.Evidence, row.GladeShape, row.GladeBehavior)
				}
			}
		})
	}
}

func containsHiddenFormatMark(value string) bool {
	return strings.ContainsAny(value, "\u200b\u200c\u200d\ufeff")
}
