// Package auditnote lets the module that authorizes a request tell the audit log which project it touched: the
// audit router opens a note in the request context and the project guard writes the project it allowed. Routes
// without a project key in their path (a test case, a step, a run) are attributed this way.
package auditnote

import "context"

type key struct{}

// Note is what the request told the audit log.
type Note struct {
	projectID   int64
	created     string
	testCaseKey string
}

// Open returns a context carrying an empty note.
func Open(ctx context.Context) (context.Context, *Note) {
	n := &Note{}
	return context.WithValue(ctx, key{}, n), n
}

// Project records the project the request was allowed into; the first one wins. Without a note it does nothing.
func Project(ctx context.Context, id int64) {
	if n, ok := ctx.Value(key{}).(*Note); ok && n.projectID == 0 {
		n.projectID = id
	}
}

// ProjectID is the project recorded, 0 when none.
func (n *Note) ProjectID() int64 { return n.projectID }

// Created names what a creation made (a project key, a test case key), so the entry says which one. A test case also
// makes the entry findable by its key. Without a note it does nothing.
func Created(ctx context.Context, name string, testCase bool) {
	if n, ok := ctx.Value(key{}).(*Note); ok {
		n.created = name
		if testCase {
			n.testCaseKey = name
		}
	}
}

// Created is the name recorded by Created, empty when none.
func (n *Note) Created() string { return n.created }

// TestCaseKey is the key of a test case the request created, empty when none.
func (n *Note) TestCaseKey() string { return n.testCaseKey }
