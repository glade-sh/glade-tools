package lwcparity

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

var markdownCodeSpanRE = regexp.MustCompile("`([^`]+)`")

func addMembersFromDoc(rows map[string]Row, id string, options memberInventoryOptions) error {
	if id == "" {
		return nil
	}
	source := localDocPathForTarget(options.DocsDir, options.Target)
	if source == "" {
		return nil
	}
	content, err := os.ReadFile(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	members := documentedMembers(string(content), options.Container)
	if len(members) == 0 {
		return nil
	}
	row := rows[id]
	row.Members = mergeMembers(row.Members, members)
	rows[id] = row
	return nil
}

func documentedMembers(content, container string) []string {
	seen := map[string]bool{}
	for _, match := range markdownCodeSpanRE.FindAllStringSubmatch(content, -1) {
		if len(match) < 2 {
			continue
		}
		member := cleanMemberName(match[1])
		if !isDocumentedMemberName(member, container) {
			continue
		}
		seen[member] = true
	}
	return sortedStringSet(seen)
}

func cleanMemberNames(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		member := cleanMemberName(value)
		if member == "" {
			continue
		}
		seen[member] = true
	}
	return sortedStringSet(seen)
}

func cleanMemberName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "`")
	value = strings.Trim(value, ".,;:()[]{}")
	return strings.TrimSpace(value)
}

func isDocumentedMemberName(value, container string) bool {
	if value == "" || value == container {
		return false
	}
	if strings.Contains(value, "/") || strings.Contains(value, " ") {
		return false
	}
	if strings.HasPrefix(value, "@") || strings.HasPrefix(value, "<") {
		return false
	}
	first := value[0]
	return first == '_' || first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z'
}

func mergeMembers(existing, additions []string) []string {
	seen := map[string]bool{}
	for _, value := range existing {
		member := cleanMemberName(value)
		if member != "" {
			seen[member] = true
		}
	}
	for _, value := range additions {
		member := cleanMemberName(value)
		if member != "" {
			seen[member] = true
		}
	}
	return sortedStringSet(seen)
}

func sortedStringSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
