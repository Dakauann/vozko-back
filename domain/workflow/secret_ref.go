package workflow

import (
	"regexp"
	"sort"
)

const SecretScope = "secret"

var codeSecretGetPattern = regexp.MustCompile(`\bsecrets\.get\(\s*(?:'([^'\\]*)'|"([^"\\]*)")\s*\)`)

func SecretRefs(config map[string]interface{}) []string {
	seen := make(map[string]bool)
	var names []string
	for _, dep := range ExtractDependencies(config) {
		if dep.Scope != SecretScope || dep.Key == "" || seen[dep.Key] {
			continue
		}
		seen[dep.Key] = true
		names = append(names, dep.Key)
	}
	sort.Strings(names)
	return names
}

func CodeSecretNames(code string) []string {
	seen := make(map[string]bool)
	var names []string
	for _, m := range codeSecretGetPattern.FindAllStringSubmatch(code, -1) {
		name := m[1] + m[2]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}
