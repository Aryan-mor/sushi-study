package catalog

import "regexp"

var (
	dubLabel     = regexp.MustCompile(`(?:^|[^a-z0-9])(?:dub(?:bed)?|duble)(?:$|[^a-z0-9])`)
	softSubLabel = regexp.MustCompile(`soft[\s_-]*sub`)
	hardSubLabel = regexp.MustCompile(`hard[\s_-]*sub`)
	hdtsLabel    = regexp.MustCompile(`(^|[^a-z])hdts([^a-z]|$)`)
)
