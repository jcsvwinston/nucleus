package observability

import "strings"

// Filter narrows what a subscriber wants to receive from the bus. Zero value
// (Filter{}) means "everything". Filter values are immutable once a
// subscription has been created — copy-on-write if you need to change them.
type Filter struct {
	// Kinds restricts to these event kinds. Empty = all kinds.
	Kinds []EventKind

	// NodeIDs restricts to events from these node identifiers. Empty = all
	// nodes. Useful for the admin server when it has aggregated events from
	// many agents and the UI only wants one node's view.
	NodeIDs []string
}

// Matches returns true if the event passes the filter. The intent is to be
// allocation-free: it walks short slices in place rather than building sets.
func (f Filter) Matches(e Event) bool {
	if e == nil {
		return false
	}
	if !f.matchesKind(e.Kind()) {
		return false
	}
	if !f.matchesNode(e.NodeID()) {
		return false
	}
	return true
}

func (f Filter) matchesKind(k EventKind) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	for _, want := range f.Kinds {
		if want == k {
			return true
		}
	}
	return false
}

func (f Filter) matchesNode(nodeID string) bool {
	if len(f.NodeIDs) == 0 {
		return true
	}
	target := strings.TrimSpace(nodeID)
	for _, want := range f.NodeIDs {
		if strings.EqualFold(strings.TrimSpace(want), target) {
			return true
		}
	}
	return false
}
