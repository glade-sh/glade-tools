package oracleprobe

import (
	"fmt"
	"strings"
)

const marker = "GLADE_STDLIB_ORACLE:"

func RenderAnonymous(cases []Case) string {
	var b strings.Builder
	b.WriteString("List<Object> gladeRows = new List<Object>();\n")
	for _, tc := range cases {
		b.WriteString("try {\n")
		for _, statement := range tc.Statements {
			b.WriteString("  ")
			b.WriteString(statement)
			if !strings.HasSuffix(strings.TrimSpace(statement), ";") {
				b.WriteString(";")
			}
			b.WriteString("\n")
		}
		b.WriteString("  Object gladeValue = ")
		b.WriteString(tc.Expression)
		b.WriteString(";\n")
		b.WriteString(fmt.Sprintf("  gladeRows.add(new Map<String,Object>{'id'=>'%s','area'=>'%s','api'=>'%s','mode'=>'%s','value'=>String.valueOf(gladeValue),'valueType'=>'%s'});\n",
			escapeApexString(tc.ID), escapeApexString(tc.Area), escapeApexString(tc.API), tc.Mode, escapeApexString(tc.ValueType)))
		b.WriteString("} catch (Exception e) {\n")
		b.WriteString(fmt.Sprintf("  gladeRows.add(new Map<String,Object>{'id'=>'%s','area'=>'%s','api'=>'%s','mode'=>'%s','exceptionType'=>e.getTypeName(),'exceptionMessage'=>e.getMessage()});\n",
			escapeApexString(tc.ID), escapeApexString(tc.Area), escapeApexString(tc.API), tc.Mode))
		b.WriteString("}\n")
	}
	b.WriteString("System.debug('")
	b.WriteString(marker)
	b.WriteString("' + JSON.serialize(gladeRows));\n")
	return b.String()
}

func escapeApexString(text string) string {
	text = strings.ReplaceAll(text, "\\", "\\\\")
	return strings.ReplaceAll(text, "'", "\\'")
}
