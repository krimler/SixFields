// Package style holds the repo's writing rules as tests.
//
// Two of them are absolute: no em dashes, and no sentences built on a contrast.
// Both are habits of generated prose, and both survive review easily because each
// instance looks harmless. A lint is the only thing that keeps them out.
//
// The rules cover this project's own code and documentation. They do not cover
// docs/build-spec.md, PLAN.md or CLAUDE.md, which are the specification this was
// built from and are someone else's words, or testdata/cassettes, which is
// recorded model output and is evidence rather than prose.
package style
