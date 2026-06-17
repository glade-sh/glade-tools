package compat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	lwcCompareScriptTag      = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	lwcCompareStyleTag       = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	lwcCompareHTMLComment    = regexp.MustCompile(`(?is)<!--.*?-->`)
	lwcCompareHTMLTag        = regexp.MustCompile(`(?is)<[^>]+>`)
	lwcCompareEntitySpace    = regexp.MustCompile(`&nbsp;|&#160;`)
	lwcCompareCustomElement  = regexp.MustCompile(`(?is)</?c-([a-z][a-z0-9_-]*)\b`)
	lwcCompareWhitespace     = regexp.MustCompile(`\s+`)
	lwcCompareSalesforceID   = regexp.MustCompile(`\b(?:00D|005|001|003|500)[A-Za-z0-9]{12,15}\b`)
	lwcCompareGeneratedToken = regexp.MustCompile(`\b[a-zA-Z0-9_-]{32,}\b`)
	lwcCompareResourceToken  = regexp.MustCompile(`/resource/\d{10,}/`)
)

func compareLwcCapturedEvidence(report *LwcCaptureReport) {
	if report == nil {
		return
	}
	for i := range report.Cases {
		report.Cases[i].Comparison = compareLwcBrowserEvidenceForComponent(report.Cases[i].LocalEvidence, report.Cases[i].SalesforceEvidence, report.Cases[i].Metadata.Component)
	}
}

func compareLwcBrowserEvidence(local, salesforce *LwcCaptureEvidence) *LwcCaptureComparison {
	return compareLwcBrowserEvidenceForComponent(local, salesforce, "")
}

func compareLwcBrowserEvidenceForComponent(local, salesforce *LwcCaptureEvidence, component string) *LwcCaptureComparison {
	if local == nil || salesforce == nil || local.Status != "captured" || salesforce.Status != "captured" {
		return nil
	}
	scope := lwcComparisonScope(component, local.DOM, salesforce.DOM)
	localDOM := local.DOM
	salesforceDOM := salesforce.DOM
	if scope.Selector != "" && scope.LocalFound {
		localDOM = extractLwcComponentDOM(local.DOM, scope.Selector)
	}
	if scope.Selector != "" && scope.SalesforceFound {
		salesforceDOM = extractLwcComponentDOM(salesforce.DOM, scope.Selector)
	}
	comparison := &LwcCaptureComparison{
		OK:         true,
		Scope:      scope,
		Local:      summarizeLwcBrowserDOM(localDOM),
		Salesforce: summarizeLwcBrowserDOM(salesforceDOM),
	}
	if scope.Selector != "" && (!scope.LocalFound || !scope.SalesforceFound) {
		comparison.Diffs = append(comparison.Diffs, LwcCaptureDiff{
			Field:      "scope",
			Local:      fmt.Sprintf("%t", scope.LocalFound),
			Salesforce: fmt.Sprintf("%t", scope.SalesforceFound),
		})
	}
	if comparison.Local.VisibleText != comparison.Salesforce.VisibleText {
		comparison.Diffs = append(comparison.Diffs, LwcCaptureDiff{
			Field:      "visibleText",
			Local:      lwcComparisonPreview(comparison.Local.VisibleText),
			Salesforce: lwcComparisonPreview(comparison.Salesforce.VisibleText),
		})
	}
	if comparison.Local.MountedComponentCount != comparison.Salesforce.MountedComponentCount {
		comparison.Diffs = append(comparison.Diffs, LwcCaptureDiff{
			Field:      "mountedComponentCount",
			Local:      fmt.Sprintf("%d", comparison.Local.MountedComponentCount),
			Salesforce: fmt.Sprintf("%d", comparison.Salesforce.MountedComponentCount),
		})
	}
	comparison.DiffCount = len(comparison.Diffs)
	comparison.OK = comparison.DiffCount == 0
	return comparison
}

func lwcComparisonScope(component, localDOM, salesforceDOM string) LwcCaptureComparisonScope {
	selector := lwcComponentSelector(component)
	if selector == "" {
		return LwcCaptureComparisonScope{}
	}
	return LwcCaptureComparisonScope{
		Selector:        selector,
		LocalFound:      extractLwcComponentDOM(localDOM, selector) != "",
		SalesforceFound: extractLwcComponentDOM(salesforceDOM, selector) != "",
	}
}

func lwcComponentSelector(component string) string {
	component = strings.TrimSpace(component)
	if component == "" {
		return ""
	}
	if strings.Contains(component, ":") {
		parts := strings.Split(component, ":")
		component = parts[len(parts)-1]
	}
	component = strings.TrimPrefix(component, "c-")
	component = strings.TrimPrefix(component, "c__")
	kebab := lwcCamelToKebab(component)
	if kebab == "" {
		return ""
	}
	return "c-" + kebab
}

func lwcCamelToKebab(value string) string {
	var b strings.Builder
	for i, r := range value {
		if r == '_' || r == ' ' {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
			continue
		}
		if r >= 'A' && r <= 'Z' {
			if i > 0 && b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "-")
}

func extractLwcComponentDOM(dom, selector string) string {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "" {
		return ""
	}
	pattern := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(selector) + `\b[^>]*>.*?</` + regexp.QuoteMeta(selector) + `>`)
	if match := pattern.FindString(dom); match != "" {
		return match
	}
	openPattern := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(selector) + `\b[^>]*>`)
	return openPattern.FindString(dom)
}

func summarizeLwcBrowserDOM(dom string) LwcCaptureComparisonSide {
	components := lwcMountedComponents(dom)
	return LwcCaptureComparisonSide{
		VisibleText:           normalizeLwcVisibleText(dom),
		MountedComponentCount: len(components),
		Components:            components,
	}
}

func normalizeLwcVisibleText(dom string) string {
	text := lwcCompareScriptTag.ReplaceAllString(dom, " ")
	text = lwcCompareStyleTag.ReplaceAllString(text, " ")
	text = lwcCompareHTMLComment.ReplaceAllString(text, " ")
	text = lwcCompareHTMLTag.ReplaceAllString(text, " ")
	text = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(text)
	text = lwcCompareEntitySpace.ReplaceAllString(text, " ")
	text = lwcCompareSalesforceID.ReplaceAllString(text, " <SF_ID> ")
	text = lwcCompareResourceToken.ReplaceAllString(text, "/resource/")
	text = lwcCompareGeneratedToken.ReplaceAllStringFunc(text, func(token string) string {
		if strings.Contains(token, "-") || strings.Contains(token, "_") {
			return " <TOKEN> "
		}
		return token
	})
	text = lwcCompareWhitespace.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func lwcMountedComponents(dom string) []string {
	seen := map[string]bool{}
	for _, match := range lwcCompareCustomElement.FindAllStringSubmatch(dom, -1) {
		if len(match) < 2 {
			continue
		}
		tag := strings.ToLower(strings.TrimPrefix(strings.Trim(match[0], "</ \t\r\n"), "/"))
		tag = strings.Fields(tag)[0]
		if strings.HasPrefix(tag, "c-") {
			seen[tag] = true
		}
	}
	components := make([]string, 0, len(seen))
	for tag := range seen {
		components = append(components, tag)
	}
	sort.Strings(components)
	return components
}

func lwcComparisonPreview(value string) string {
	const limit = 240
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
