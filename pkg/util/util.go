package util

import "strings"

func ContainsFold(s, v string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(v))
}
