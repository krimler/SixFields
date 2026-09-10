package explain

import (
	"fmt"
	"sort"
	"strings"
)

// Anonymizer replaces object and namespace names with keys before anything leaves
// the machine, and puts them back in the answer. It is reversible on purpose: the
// user reads their own names, the model never sees them.
//
// With the local backend nothing leaves the machine and this defaults off; with
// the anthropic backend it defaults on (docs/ai.md says which is active).
type Anonymizer struct {
	forward map[string]string
	reverse map[string]string
	next    int
}

func NewAnonymizer() *Anonymizer {
	return &Anonymizer{forward: map[string]string{}, reverse: map[string]string{}}
}

// Learn registers a name. Longer names are replaced first, so a name that
// contains another is not half-substituted.
func (a *Anonymizer) Learn(names ...string) {
	for _, name := range names {
		if name == "" || a.forward[name] != "" {
			continue
		}
		a.next++
		key := fmt.Sprintf("obj-%03d", a.next)
		a.forward[name] = key
		a.reverse[key] = name
	}
}

func (a *Anonymizer) Hide(text string) string   { return replaceAll(text, a.forward) }
func (a *Anonymizer) Reveal(text string) string { return replaceAll(text, a.reverse) }

// HideRequest returns a copy of the request with every learned name replaced.
func (a *Anonymizer) HideRequest(req Request) Request {
	out := req
	out.Stall.Object.Name = a.Hide(req.Stall.Object.Name)
	out.Stall.Object.Namespace = a.Hide(req.Stall.Object.Namespace)
	out.Stall.Message = a.Hide(req.Stall.Message)
	out.Stall.FullMessage = a.Hide(req.Stall.FullMessage)
	out.Stall.Raw = a.Hide(req.Stall.Raw)
	out.Runbook = a.Hide(req.Runbook)
	out.Names = make([]string, len(req.Names))
	for i, n := range req.Names {
		out.Names[i] = a.Hide(n)
	}
	return out
}

// RevealExplanation puts the real names back before the user reads it.
func (a *Anonymizer) RevealExplanation(e Explanation) Explanation {
	out := e
	out.Lines = make([]string, len(e.Lines))
	for i, line := range e.Lines {
		out.Lines[i] = a.Reveal(line)
	}
	out.NextCommand = a.Reveal(e.NextCommand)
	return out
}

func replaceAll(text string, table map[string]string) string {
	if text == "" || len(table) == 0 {
		return text
	}
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		text = strings.ReplaceAll(text, k, table[k])
	}
	return text
}
