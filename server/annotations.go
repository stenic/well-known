package main

import "regexp"

func resolveName(name string) string {
	r := regexp.MustCompile(`^well-known.stenic.io/(.+)$`)
	if !r.MatchString(name) {
		return ""
	}
	m := r.FindStringSubmatch(name)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}
