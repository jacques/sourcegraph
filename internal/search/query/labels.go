package query

import "sort"

// Labels are general-purpose annotations that store information about a node.
type labels uint16

const (
	None    labels = 0
	Literal labels = 1 << iota
	Regexp
	Quoted
	// SingleQuoted will be set (in addition to Quoted) when the pattern was wrapped in single quotes '...'
	SingleQuoted
	HeuristicParensAsPatterns
	HeuristicDanglingParens
	HeuristicHoisted
	Structural
	IsPredicate
	// IsAlias flags whether the original syntax referred to an alias rather
	// than canonical form (r: instead of repo:)
	IsAlias
	Standard
	QuotesAsLiterals
	Boost
	// IsContent is set on patterns that come from content:
	IsContent
)

var allLabels = map[labels]string{
	None:                      "None",
	Literal:                   "Literal",
	Regexp:                    "Regexp",
	Quoted:                    "Quoted",
	HeuristicParensAsPatterns: "HeuristicParensAsPatterns",
	HeuristicDanglingParens:   "HeuristicDanglingParens",
	HeuristicHoisted:          "HeuristicHoisted",
	Structural:                "Structural",
	IsPredicate:               "IsPredicate",
	IsAlias:                   "IsAlias",
	Standard:                  "Standard",
	QuotesAsLiterals:          "QuotesAsLiterals",
	Boost:                     "Boost",
	IsContent:                 "IsContent",
}

func (l *labels) IsSet(label labels) bool {
	return *l&label != 0
}

func (l *labels) Set(label labels) {
	*l |= label
}

func (l *labels) Unset(label labels) {
	*l &^= label
}

func (l *labels) String() []string {
	if *l == 0 {
		return []string{"None"}
	}
	var s []string
	for k, v := range allLabels {
		if l.IsSet(k) {
			s = append(s, v)
		}
	}
	sort.Strings(s)
	return s
}
