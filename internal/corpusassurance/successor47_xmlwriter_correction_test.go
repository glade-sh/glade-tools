package corpusassurance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSuccessor47XMLWriterMatchesSalesforceOutput(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-xmlstreamwriter-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Evidence) != 18 || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Command.Args[0] != fixture.Source[0].Content {
		t.Fatalf("fixture contract = %d rows, command/source %#v/%#v", len(fixture.Evidence), fixture.Command, fixture.Source)
	}
	for _, row := range fixture.Evidence {
		if row.Kind != "exec" {
			t.Fatalf("evidence %s kind = %q, want exec", row.SurfaceID, row.Kind)
		}
	}

	const exactXML = `<?xml version="1.0" encoding="UTF-8"?><root xmlns="urn:base" xmlns:x="urn:x" x:id="A&amp;B"><!--note--><?pi go?><![CDATA[<raw>]]><x:child>Tom &amp; Sue</x:child><leaf/></root>`
	source := fixture.Source[0].Content
	for _, assertion := range []string{
		"System.assertEquals('" + exactXML + "', xml);",
		"System.assertEquals('System.XmlStreamWriter[]', writer.toString());",
	} {
		if !strings.Contains(source, assertion) {
			t.Fatalf("XML writer source missing exact assertion %q", assertion)
		}
	}
	for _, stale := range []string{"xml.contains(", "xml.endsWith(", "System.assertEquals(xml, writer.toString())"} {
		if strings.Contains(source, stale) {
			t.Fatalf("XML writer source retains stale assertion %q", stale)
		}
	}
}
