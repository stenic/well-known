package main

import "regexp"

var annotationRegex = regexp.MustCompile(`^well-known.stenic.io/(.+)$`)

func resolveName(name string) string {
	if !annotationRegex.MatchString(name) {
		return ""
	}
	m := annotationRegex.FindStringSubmatch(name)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}
