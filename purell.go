/*
Package purell offers URL normalization as described on the wikipedia page:
http://en.wikipedia.org/wiki/URL_normalization
*/
package purell

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A set of normalization flags determines how a URL will
// be normalized.
type NormalizationFlags uint

const (
	// Safe normalizations
	FlagLowercaseScheme NormalizationFlags = 1 << iota
	FlagLowercaseHost
	FlagUppercaseEscapes
	FlagDecodeUnnecessaryEscapes
	FlagRemoveDefaultPort
	FlagRemoveEmptyQuerySeparator

	// Usually safe normalizations
	FlagRemoveTrailingSlash // Should choose one or the other (in add-remove slash)
	FlagAddTrailingSlash
	FlagRemoveDotSegments

	// Unsafe normalizations
	FlagRemoveDirectoryIndex
	FlagRemoveFragment
	FlagForceHTTP
	FlagRemoveDuplicateSlashes
	FlagRemoveWWW // Should choose one or the other (in add-remove www)
	FlagAddWWW
	FlagSortQuery

	// Normalizations not in the wikipedia article, required to cover tests cases
	// submitted by jehiah (not included in any convenience set at the moment)
	FlagDecodeDWORDHost
	FlagDecodeOctalHost
	FlagDecodeHexHost
	FlagRemoveUnnecessaryHostDots
	FlagRemoveEmptyPortSeparator

	FlagsSafe NormalizationFlags = FlagLowercaseHost | FlagLowercaseScheme | FlagUppercaseEscapes | FlagDecodeUnnecessaryEscapes | FlagRemoveDefaultPort | FlagRemoveEmptyQuerySeparator

	FlagsUsuallySafe NormalizationFlags = FlagsSafe | FlagRemoveTrailingSlash | FlagRemoveDotSegments

	FlagsUnsafe NormalizationFlags = FlagsUsuallySafe | FlagRemoveDirectoryIndex | FlagRemoveFragment | FlagForceHTTP | FlagRemoveDuplicateSlashes | FlagRemoveWWW | FlagSortQuery
)

const (
	defaultHttpPort  = ":80"
	defaultHttpsPort = ":443"
)

// Regular expressions used by the normalizations
var rxPort = regexp.MustCompile(`(:\d+)/?$`)
var rxDirIndex = regexp.MustCompile(`(^|/)((?:default|index)\.\w{1,4})$`)
var rxDupSlashes = regexp.MustCompile(`/{2,}`)
var rxDWORDHost = regexp.MustCompile(`^(\d+)((?:\.+)?(?:\:\d*)?)$`)
var rxOctalHost = regexp.MustCompile(`^(0\d*)\.(0\d*)\.(0\d*)\.(0\d*)((?:\.+)?(?:\:\d*)?)$`)
var rxHexHost = regexp.MustCompile(`^0x([0-9A-Fa-f]+)((?:\.+)?(?:\:\d*)?)$`)
var rxHostDots = regexp.MustCompile(`^(.+?)(:\d+)?$`)
var rxEmptyPort = regexp.MustCompile(`:+$`)

// Map of flags to implementation function.
// FlagDecodeUnnecessaryEscapes has no action, since it is done automatically
// by parsing the string as an URL. Same for FlagUppercaseEscapes and FlagRemoveEmptyQuerySeparator.

// Since maps have undefined traversing order, make a slice of ordered keys
var flagsOrder = []NormalizationFlags{
	FlagLowercaseScheme,
	FlagLowercaseHost,
	FlagRemoveDefaultPort,
	FlagRemoveDirectoryIndex,
	FlagRemoveDotSegments,
	FlagRemoveFragment,
	FlagForceHTTP, // Must be after remove default port (because https=443/http=80)
	FlagRemoveDuplicateSlashes,
	FlagRemoveWWW,
	FlagAddWWW,
	FlagSortQuery,
	FlagDecodeDWORDHost,
	FlagDecodeOctalHost,
	FlagDecodeHexHost,
	FlagRemoveUnnecessaryHostDots,
	FlagRemoveEmptyPortSeparator,
	FlagRemoveTrailingSlash, // These two (add/remove trailing slash) must be last
	FlagAddTrailingSlash,
}

// ... and then the map, where order is unimportant
var flags = map[NormalizationFlags]func(*url.URL){
	FlagLowercaseScheme:           lowercaseScheme,
	FlagLowercaseHost:             lowercaseHost,
	FlagRemoveDefaultPort:         removeDefaultPort,
	FlagRemoveDirectoryIndex:      removeDirectoryIndex,
	FlagRemoveDotSegments:         removeDotSegments,
	FlagRemoveFragment:            removeFragment,
	FlagForceHTTP:                 forceHTTP,
	FlagRemoveDuplicateSlashes:    removeDuplicateSlashes,
	FlagRemoveWWW:                 removeWWW,
	FlagAddWWW:                    addWWW,
	FlagSortQuery:                 sortQuery,
	FlagDecodeDWORDHost:           decodeDWORDHost,
	FlagDecodeOctalHost:           decodeOctalHost,
	FlagDecodeHexHost:             decodeHexHost,
	FlagRemoveUnnecessaryHostDots: removeUnncessaryHostDots,
	FlagRemoveEmptyPortSeparator:  removeEmptyPortSeparator,
	FlagRemoveTrailingSlash:       removeTrailingSlash,
	FlagAddTrailingSlash:          addTrailingSlash,
}

// MustNormalizeURLString returns the normalized string, and panics if an error occurs.
// It takes an URL string as input, as well as the normalization flags.
func MustNormalizeURLString(u string, f NormalizationFlags) string {
	if parsed, e := url.Parse(u); e != nil {
		panic(e)
	} else {
		return NormalizeURL(parsed, f)
	}
	panic("Unreachable code.")
}

// NormalizeURLString returns the normalized string, or an error if it can't be parsed into an URL object.
// It takes an URL string as input, as well as the normalization flags.
func NormalizeURLString(u string, f NormalizationFlags) (string, error) {
	if parsed, e := url.Parse(u); e != nil {
		return "", e
	} else {
		return NormalizeURL(parsed, f), nil
	}
	panic("Unreachable code.")
}

// NormalizeURL returns the normalized string.
// It takes a parsed URL object as input, as well as the normalization flags.
func NormalizeURL(u *url.URL, f NormalizationFlags) string {
	for _, k := range flagsOrder {
		if f&k == k {
			flags[k](u)
		}
	}
	return u.String()
}

func lowercaseScheme(u *url.URL) {
	if len(u.Scheme) > 0 {
		u.Scheme = strings.ToLower(u.Scheme)
	}
}

func lowercaseHost(u *url.URL) {
	if len(u.Host) > 0 {
		u.Host = strings.ToLower(u.Host)
	}
}

func removeDefaultPort(u *url.URL) {
	if len(u.Host) > 0 {
		scheme := strings.ToLower(u.Scheme)
		u.Host = rxPort.ReplaceAllStringFunc(u.Host, func(val string) string {
			if (scheme == "http" && val == defaultHttpPort) || (scheme == "https" && val == defaultHttpsPort) {
				return ""
			}
			return val
		})
	}
}

func removeTrailingSlash(u *url.URL) {
	if l := len(u.Path); l > 0 && strings.HasSuffix(u.Path, "/") {
		u.Path = u.Path[:l-1]
	} else if l = len(u.Host); l > 0 && strings.HasSuffix(u.Host, "/") {
		u.Host = u.Host[:l-1]
	}
}

func addTrailingSlash(u *url.URL) {
	if l := len(u.Path); l > 0 && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	} else if l = len(u.Host); l > 0 && !strings.HasSuffix(u.Host, "/") {
		u.Host += "/"
	}
}

func removeDotSegments(u *url.URL) {
	if len(u.Path) > 0 {
		var dotFree []string
		var lastIsDot bool

		sections := strings.Split(u.Path, "/")
		for _, s := range sections {
			if s == ".." {
				if len(dotFree) > 0 {
					dotFree = dotFree[:len(dotFree)-1]
				}
			} else if s != "." {
				dotFree = append(dotFree, s)
			}
			lastIsDot = (s == "." || s == "..")
		}
		// Special case if host does not end with / and new path does not begin with /
		u.Path = strings.Join(dotFree, "/")
		if !strings.HasSuffix(u.Host, "/") && !strings.HasPrefix(u.Path, "/") {
			u.Path = "/" + u.Path
		}
		// Special case if the last segment was a dot, make sure the path ends with a slash
		if lastIsDot && !strings.HasSuffix(u.Path, "/") {
			u.Path += "/"
		}
	}
}

func removeDirectoryIndex(u *url.URL) {
	if len(u.Path) > 0 {
		u.Path = rxDirIndex.ReplaceAllString(u.Path, "$1")
	}
}

func removeFragment(u *url.URL) {
	u.Fragment = ""
}

func forceHTTP(u *url.URL) {
	if strings.ToLower(u.Scheme) == "https" {
		u.Scheme = "http"
	}
}

func removeDuplicateSlashes(u *url.URL) {
	if len(u.Path) > 0 {
		u.Path = rxDupSlashes.ReplaceAllString(u.Path, "/")
	}
}

func removeWWW(u *url.URL) {
	if len(u.Host) > 0 && strings.HasPrefix(strings.ToLower(u.Host), "www.") {
		u.Host = u.Host[4:]
	}
}

func addWWW(u *url.URL) {
	if len(u.Host) > 0 && !strings.HasPrefix(strings.ToLower(u.Host), "www.") {
		u.Host = "www." + u.Host
	}
}

func sortQuery(u *url.URL) {
	q := u.Query()

	if len(q) > 0 {
		arKeys := make([]string, len(q))
		i := 0
		for k, _ := range q {
			arKeys[i] = k
			i++
		}
		sort.Strings(arKeys)
		buf := new(bytes.Buffer)
		for _, k := range arKeys {
			sort.Strings(q[k])
			for _, v := range q[k] {
				if buf.Len() > 0 {
					buf.WriteRune('&')
				}
				buf.WriteString(fmt.Sprintf("%s=%s", k, url.QueryEscape(v)))
			}
		}

		// Rebuild the raw query string
		u.RawQuery = buf.String()
	}
}

func decodeDWORDHost(u *url.URL) {
	if len(u.Host) > 0 {
		if matches := rxDWORDHost.FindStringSubmatch(u.Host); len(matches) > 2 {
			var parts [4]int64

			dword, _ := strconv.ParseInt(matches[1], 10, 0)
			for i, shift := range []uint{24, 16, 8, 0} {
				parts[i] = dword >> shift & 0xFF
			}
			u.Host = fmt.Sprintf("%d.%d.%d.%d%s", parts[0], parts[1], parts[2], parts[3], matches[2])
		}
	}
}

func decodeOctalHost(u *url.URL) {
	if len(u.Host) > 0 {
		if matches := rxOctalHost.FindStringSubmatch(u.Host); len(matches) > 5 {
			var parts [4]int64

			for i := 1; i <= 4; i++ {
				parts[i-1], _ = strconv.ParseInt(matches[i], 8, 0)
			}
			u.Host = fmt.Sprintf("%d.%d.%d.%d%s", parts[0], parts[1], parts[2], parts[3], matches[5])
		}
	}
}

func decodeHexHost(u *url.URL) {
	if len(u.Host) > 0 {
		if matches := rxHexHost.FindStringSubmatch(u.Host); len(matches) > 2 {
			// Conversion is safe because of regex validation
			parsed, _ := strconv.ParseInt(matches[1], 16, 0)
			// Set host as DWORD (base 10) encoded host
			u.Host = fmt.Sprintf("%d%s", parsed, matches[2])
			// The rest is the same as decoding a DWORD host
			decodeDWORDHost(u)
		}
	}
}

func removeUnncessaryHostDots(u *url.URL) {
	if len(u.Host) > 0 {
		if matches := rxHostDots.FindStringSubmatch(u.Host); len(matches) > 1 {
			// Trim the leading and trailing dots
			u.Host = strings.Trim(matches[1], ".")
			if len(matches) > 2 {
				u.Host += matches[2]
			}
		}
	}
}

func removeEmptyPortSeparator(u *url.URL) {
	if len(u.Host) > 0 {
		u.Host = rxEmptyPort.ReplaceAllString(u.Host, "")
	}
}
