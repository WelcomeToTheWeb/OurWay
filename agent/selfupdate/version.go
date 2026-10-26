package selfupdate

import (
	"os"
	"strconv"
	"strings"
)

// The update policy follows docs/versioning.md: an agent takes an update only
// when the server's version ORDERS ABOVE its own, and only on its channel.
// Comparing strings for inequality is not a policy — a server stamped per
// commit is "different" on every push, which would re-update the whole fleet
// dozens of times a day with no ordering and no way to say which is newer.

const (
	channelStable = "stable"
	channelEdge   = "edge"
)

// version is a parsed release string. Unparsed versions (a local "dev"
// build, a raw commit sha, an empty string) carry no ordering and never
// trigger an update, in either direction.
type version struct {
	major, minor, patch uint64
	// pre is the ordered prerelease tuple; empty means a release.
	pre []string
	// ok is false when the string is not X.Y.Z[-pre][+meta].
	ok bool
}

// parseVersion reads "X.Y.Z", "vX.Y.Z", "X.Y.Z-pre.1+meta".
func parseVersion(raw string) version {
	var v version
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "v"))
	if s == "" {
		return v
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata never orders anything
	}
	var prePart string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		prePart, s = s[i+1:], s[:i]
		// "2.0.3-" and "2.0.3-+meta" are malformed, not bare releases.
		if prePart == "" {
			return v
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v
	}
	nums := make([]uint64, 3)
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || strings.Trim(p, "0123456789") != "" {
			return v
		}
		nums[i] = n
	}
	v.major, v.minor, v.patch = nums[0], nums[1], nums[2]
	if prePart != "" {
		for _, id := range strings.Split(prePart, ".") {
			if id == "" {
				return v
			}
			v.pre = append(v.pre, id)
		}
	}
	v.ok = true
	return v
}

// compare orders two versions: -1 when a is older, 0 equal, 1 newer.
// An unparsed version never compares: both sides must parse.
func (a version) compare(b version) (int, bool) {
	if !a.ok || !b.ok {
		return 0, false
	}
	for _, pair := range [3][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1, true
			}
			return 1, true
		}
	}
	return comparePrerelease(a.pre, b.pre), true
}

// comparePrerelease applies semver's rule: a release outranks any of its
// prereleases, and between prereleases, numeric identifiers compare
// numerically and rank below alphanumeric ones.
func comparePrerelease(a, b []string) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
	}
	if len(b) == 0 {
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		aNum, an := numericID(a[i])
		bNum, bn := numericID(b[i])
		if aNum && bNum {
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
			continue
		}
		if aNum {
			return -1
		}
		if bNum {
			return 1
		}
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) == len(b):
		return 0
	case len(a) < len(b):
		return -1
	default:
		return 1
	}
}

// numericID reports whether a prerelease identifier is purely numeric, and
// its value when it is.
func numericID(id string) (bool, uint64) {
	if strings.Trim(id, "0123456789") != "" {
		return false, 0
	}
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil, n
}

// updateChannel is the agent's update policy, from OURWAY_UPDATE_CHANNEL.
// Anything unrecognised is stable, so a typo can never silently move a fleet
// onto main builds.
func updateChannel() string {
	c := strings.ToLower(strings.TrimSpace(os.Getenv("OURWAY_UPDATE_CHANNEL")))
	if c == channelEdge {
		return channelEdge
	}
	return channelStable
}

// shouldUpdate reports whether an agent running local should take serverVer.
// Rules, in order:
//  1. both sides must parse, or there is no update;
//  2. a stable agent ignores prerelease servers entirely;
//  3. the server must order strictly above the local build — equal is done,
//     lower is never automatic.
func shouldUpdate(serverVer, local, channel string) bool {
	server, localV := parseVersion(serverVer), parseVersion(local)
	cmp, ok := server.compare(localV)
	if !ok || cmp <= 0 {
		return false
	}
	if channel != channelEdge && len(server.pre) > 0 {
		return false
	}
	return true
}
