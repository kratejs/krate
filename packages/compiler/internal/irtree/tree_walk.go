package irtree

// HasClientComponent reports whether slots, or anything nested in them,
// render a client-tier component: one with signals or handlers that hydrates
// in the browser.
func HasClientComponent(slots []SlotNode) bool {
	for _, s := range slots {
		switch n := s.(type) {
		case *ComponentSlot:
			if n.Component == nil {
				continue
			}
			if n.Component.Tier == TierClient || HasClientComponent(n.Component.Children) || HasClientComponent(n.Component.ReturnSlots) {
				return true
			}
		case *ConditionalSlot:
			if HasClientComponent(n.Consequent) || HasClientComponent(n.Alternate) {
				return true
			}
		case *ListSlot:
			for _, item := range n.Items {
				if item != nil && HasClientComponent(item.Contents) {
					return true
				}
			}
		case *SuspenseSlot:
			if HasClientComponent(n.Fallback) || HasClientComponent(n.Resolved) {
				return true
			}
		case *MetaSlot:
			if HasClientComponent(n.Children) {
				return true
			}
		}
	}
	return false
}
