package celenv

import (
	"regexp"
	"sync"
)

// maxRegexps bounds the cache; past it the cache starts over, which costs
// a recompile, never a wrong answer.
const maxRegexps = 1024

var (
	regexMu  sync.Mutex
	regexps  = map[string]*regexp.Regexp{}
	regexErr = map[string]error{}
)

// compileRegexp compiles a pattern once per process: rules call matches,
// grep and scan once per item of a foreach, with the same pattern.
func compileRegexp(pattern string) (*regexp.Regexp, error) {
	regexMu.Lock()
	defer regexMu.Unlock()
	if rx, ok := regexps[pattern]; ok {
		return rx, nil
	}
	if err, ok := regexErr[pattern]; ok {
		return nil, err
	}
	if len(regexps)+len(regexErr) >= maxRegexps {
		regexps, regexErr = map[string]*regexp.Regexp{}, map[string]error{}
	}
	rx, err := regexp.Compile(pattern)
	if err != nil {
		regexErr[pattern] = err
		return nil, err
	}
	regexps[pattern] = rx
	return rx, nil
}
