package compat

import (
	"html"
	"mime"
	"regexp"
	"sort"
	"strings"
)

var visualforceDiffHTMLComment = regexp.MustCompile(`(?is)<!--.*?-->`)
var visualforceDiffInputTag = regexp.MustCompile(`(?is)<input\b[^>]*>`)
var visualforceDiffAttr = regexp.MustCompile(`(?is)\b([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*("[^"]*"|'[^']*'|[^\s"'=<>` + "`" + `]+)`)
var visualforceDiffGeneratedID = regexp.MustCompile(`\bj_id[a-zA-Z0-9_:$.-]*\b`)
var visualforceDiffTimestamp = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ][0-9:.+-]*(?:Z|[+-]\d{2}:?\d{2})?\b`)
var visualforceDiffSalesforceID = regexp.MustCompile(`\b(?:00D|005)[A-Za-z0-9]{12,15}\b`)
var visualforceDiffLongBlob = regexp.MustCompile(`\b[A-Za-z0-9+/=_-]{32,}\b`)

func visualforceDiffFieldMatches(variant, field, salesforceValue, localValue string, contractTextMatches bool) bool {
	if field == "contentType" {
		if variant == "pdf" && visualforceDiffPDFContentTypeMatches(salesforceValue, localValue) {
			return true
		}
		return visualforceDiffNormalizeContentType(salesforceValue) == visualforceDiffNormalizeContentType(localValue)
	}
	if variant == "pdf" && visualforceDiffPDFVolatileField(field) {
		return contractTextMatches
	}
	if contractTextMatches && visualforceDiffFieldUsesContractText(variant, field) {
		return true
	}
	if field == "text" {
		return normalizeVisualforceDiffContractText(salesforceValue) == normalizeVisualforceDiffContractText(localValue)
	}
	return salesforceValue == localValue
}

func visualforceDiffPDFContentTypeMatches(salesforceValue, localValue string) bool {
	salesforceType := visualforceDiffNormalizeContentType(salesforceValue)
	localType := visualforceDiffNormalizeContentType(localValue)
	if salesforceType == localType {
		return true
	}
	return salesforceType == "missing" && localType == "application/pdf" || salesforceType == "application/pdf" && localType == "missing"
}

func visualforceDiffFieldUsesContractText(variant, field string) bool {
	switch variant {
	case "html":
		switch field {
		case "bytes", "sha256", "bodyHash", "body", "textHash", "text", "normalizedText", "contractText":
			return true
		}
	case "pdf":
		switch field {
		case "bytes", "sha256", "bodyHash", "base64", "textHash", "text", "normalizedText", "contractText":
			return true
		}
	}
	return false
}

func visualforceDiffVariantMatches(variant string, salesforce, local map[string]any) bool {
	return variant == "html" && visualforceDiffRenderLooksPDF(salesforce) && visualforceDiffRenderLooksPDF(local)
}

func visualforceDiffRenderLooksPDF(fields map[string]any) bool {
	if contentType, ok := visualforceDiffValue(fields, "contentType"); ok && visualforceDiffNormalizeContentType(contentType) == "application/pdf" {
		return true
	}
	if body, ok := visualforceDiffValue(fields, "body"); ok && strings.HasPrefix(strings.TrimSpace(body), "%PDF-") {
		return true
	}
	if sha, ok := visualforceDiffValue(fields, "pdfSha256"); ok && strings.TrimSpace(sha) != "" {
		return true
	}
	status, _ := visualforceDiffValue(fields, "status")
	return strings.EqualFold(strings.TrimSpace(status), "notCaptured") && visualforceDiffNormalizeContentType(visualforceDiffStringField(fields, "contentType")) == "application/pdf"
}

func visualforceDiffStringField(fields map[string]any, name string) string {
	value, _ := visualforceDiffValue(fields, name)
	return value
}

func visualforceDiffPDFVolatileField(field string) bool {
	switch field {
	case "bytes", "sha256", "bodyHash":
		return true
	default:
		return false
	}
}

func visualforceDiffContractText(variant string, fields map[string]any) (string, bool) {
	if text, ok := visualforceDiffValue(fields, "contractText"); ok {
		return normalizeVisualforceDiffContractText(text), true
	}
	if text, ok := visualforceDiffValue(fields, "normalizedText"); ok {
		return normalizeVisualforceDiffContractText(text), true
	}
	if text, ok := visualforceDiffValue(fields, "text"); ok {
		return normalizeVisualforceDiffContractText(text), true
	}
	if variant != "html" {
		return "", false
	}
	body, ok := visualforceDiffValue(fields, "body")
	if !ok {
		return "", false
	}
	return normalizeVisualforceDiffHTMLContractText(body), true
}

func visualforceDiffNormalizeContentType(value string) string {
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(value, ";")[0])
		params = nil
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if len(params) == 0 {
		return mediaType
	}
	parts := make([]string, 0, len(params))
	for key, value := range params {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || key == "charset" {
			continue
		}
		parts = append(parts, key+"="+strings.TrimSpace(value))
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return mediaType
	}
	return mediaType + ";" + strings.Join(parts, ";")
}

func normalizeVisualforceDiffHTMLContractText(body string) string {
	text := visualforceDiffHTMLComment.ReplaceAllString(body, " ")
	text = visualforceScriptStyleBlock.ReplaceAllString(text, " ")
	text = visualforceDiffInputTag.ReplaceAllStringFunc(text, visualforceDiffInputText)
	text = visualforceHTMLTag.ReplaceAllString(text, " ")
	return normalizeVisualforceDiffContractText(text)
}

func visualforceDiffInputText(tag string) string {
	inputType, _ := visualforceDiffHTMLAttr(tag, "type")
	if strings.EqualFold(strings.TrimSpace(inputType), "hidden") {
		return " "
	}
	value, ok := visualforceDiffHTMLAttr(tag, "value")
	if !ok {
		return " "
	}
	return " " + value + " "
}

func visualforceDiffHTMLAttr(tag, name string) (string, bool) {
	for _, match := range visualforceDiffAttr.FindAllStringSubmatch(tag, -1) {
		if len(match) < 3 || !strings.EqualFold(match[1], name) {
			continue
		}
		value := strings.TrimSpace(match[2])
		if len(value) >= 2 {
			first := value[0]
			last := value[len(value)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		return html.UnescapeString(value), true
	}
	return "", false
}

func normalizeVisualforceDiffContractText(text string) string {
	text = html.UnescapeString(text)
	text = visualforceDiffSalesforceID.ReplaceAllString(text, " <SF_ID> ")
	for _, replacer := range []*regexp.Regexp{
		visualforceDiffGeneratedID,
		visualforceDiffTimestamp,
	} {
		text = replacer.ReplaceAllString(text, " ")
	}
	text = visualforceDiffLongBlob.ReplaceAllStringFunc(text, func(token string) string {
		if visualforceDiffLooksVolatileBlobToken(token) {
			return " "
		}
		return token
	})
	return strings.Join(strings.Fields(text), " ")
}

func visualforceDiffLooksVolatileBlobToken(token string) bool {
	for _, ch := range token {
		if ch < 'a' || ch > 'z' {
			return true
		}
	}
	return false
}
