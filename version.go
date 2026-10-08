package main

import "runtime/debug"

// version is the git commit the binary was built from. Go records it
// automatically when building from a git checkout. For builds without
// .git, set it with: go build -ldflags "-X main.version=$(git rev-parse HEAD)"
var version string

const repoURL = "https://github.com/fffinkel/rate-my-comms"

type buildInfo struct {
	SHA   string // full commit, empty when unknown
	Short string // first 7 characters, or "dev"
	Dirty bool
	URL   string // link to the commit on GitHub, empty when unknown
}

func readBuildInfo() buildInfo {
	b := buildInfo{SHA: version}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if b.SHA == "" {
					b.SHA = s.Value
				}
			case "vcs.modified":
				b.Dirty = s.Value == "true"
			}
		}
	}
	if b.SHA == "" {
		b.Short = "dev"
		return b
	}
	b.Short = b.SHA
	if len(b.Short) > 7 {
		b.Short = b.Short[:7]
	}
	b.URL = repoURL + "/commit/" + b.SHA
	return b
}
