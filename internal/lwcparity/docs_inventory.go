package lwcparity

import (
	"os"
	"path/filepath"
	"strings"
)

type memberInventoryOptions struct {
	DocsDir   string
	Target    string
	Container string
}

func optionsForMemberInventory(docsDir, target, container string) memberInventoryOptions {
	return memberInventoryOptions{
		DocsDir:   docsDir,
		Target:    strings.TrimSpace(target),
		Container: cleanDocName(container),
	}
}

func localDocPathForTarget(docsDir, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		return ""
	}
	target = strings.TrimPrefix(target, "/")
	base := filepath.Base(target)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	if ext := filepath.Ext(base); ext != ".md" {
		base = strings.TrimSuffix(base, ext) + ".md"
	}
	return filepath.Join(docsDir, base)
}

func resolveModuleDocTarget(docsDir, target, module string) string {
	target = strings.TrimSpace(target)
	path := localDocPathForTarget(docsDir, target)
	if path == "" {
		return target
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return target
	}
	for _, line := range strings.Split(string(content), "\n") {
		label, childTarget, ok := firstMarkdownLink(line)
		if !ok || strings.TrimSpace(childTarget) == "" {
			continue
		}
		if cleanDocName(label) == cleanDocName(module) {
			return childTarget
		}
	}
	return target
}
