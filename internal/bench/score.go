//go:build bench

package bench

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"sixfields/internal/explain"
	"sixfields/internal/msg"
	"sixfields/internal/snapshot"
)

// outcome is the k8s-bench split, and it is the column worth reading: a
// framework error is the backend failing to produce a usable answer, a reasoning
// error is a usable answer that is wrong. Reported as one number they say
// nothing, a model that is badly wired and a model that is bad at the task need
// opposite fixes.
type outcome int

const (
	pass outcome = iota
	frameworkError
	reasoningError
)

func (o outcome) String() string {
	switch o {
	case pass:
		return "pass"
	case frameworkError:
		return "framework"
	default:
		return "reasoning"
	}
}

// score decides what happened on one task.
//
// The schema is checked against the code the backend itself returned, so that
// breaking the shape and naming the wrong class are separated: the first is a
// backend that did not answer, the second is an answer that is wrong.
func score(t task, answer explain.Explanation, err error) (label outcome, detail string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return frameworkError, fmt.Sprintf("no answer within %s", explain.Timeout)
	case err != nil:
		return frameworkError, err.Error()
	}
	if _, known := msg.Get(msg.Code(answer.Code)); !known {
		return frameworkError, fmt.Sprintf("%q is not a code in the registry", answer.Code)
	}
	if err := answer.Validate(msg.Code(answer.Code)); err != nil {
		return frameworkError, err.Error()
	}
	if answer.Code != string(t.code) {
		return reasoningError, fmt.Sprintf("answered %s, the analyzer ranked %s", answer.Code, t.code)
	}
	if !namesObject(answer, t.object) {
		return reasoningError, fmt.Sprintf("named no object, the analyzer ranked %s", t.object)
	}
	return pass, ""
}

// namesObject reads the three lines the user sees, and nothing else: the
// suggested command often carries a name the prose never said, and a diagnosis
// the reader has to reconstruct from a kubectl invocation has not been given.
//
// The name identifies the object: names are unique in a namespace, kinds repeat.
// A backend that writes "DevMachine dev-3-zqx65" with a space has still named it.
func namesObject(e explain.Explanation, ref snapshot.Ref) bool {
	for _, line := range e.Lines {
		for _, field := range strings.Fields(line) {
			for _, token := range strings.Split(strings.Trim(field, ".,;:()[]\"'`"), "/") {
				if token == ref.Name {
					return true
				}
			}
		}
	}
	return false
}
