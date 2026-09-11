// Package style holds the repo's writing rules as tests.
//
// Two of them are absolute: no em dashes, and no contrast that carries nothing.
// The banned shapes are in the test. Both are habits of generated prose,
// and both survive review easily because each instance looks harmless. A lint is
// the only thing that keeps them out.
//
// "rather than" and "instead of" are deliberately allowed. They were banned here
// for a while, and the ban caught four sentences in the README that were using
// them correctly, to correct a reading the reader was likely to have: "Linux is
// untested rather than unsupported" says one thing and needs those words. A word
// that good prose needs is the wrong thing to lint.
//
// The rules cover this project's own code and documentation. They do not cover
// docs/build-spec.md, PLAN.md or CLAUDE.md, which are the specification this was
// built from and are someone else's words, or testdata/cassettes, which is
// recorded model output and is evidence rather than prose.
package style
