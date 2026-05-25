// Package fixed provides the integer-grid coordinate representation and the
// exact-arithmetic predicates used by the polyclip boolean and offset
// engines.
//
// This package lives under internal/ and is not importable outside this
// module. Public symbols here are exported so other packages within the
// module can address them; they are not part of polyclip's public API.
// See ../../DESIGN.md §5 for the numeric model.
package fixed
