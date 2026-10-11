package lang

const maxNesting = 64

type Nesting[K comparable] struct {
	closers [maxNesting]K
	depth   int
}

func (n *Nesting[K]) Depth() int { return n.depth }

func (n *Nesting[K]) Tracked() bool { return n.depth <= maxNesting }

func (n *Nesting[K]) Push(closer K) {
	if n.depth < maxNesting {
		n.closers[n.depth] = closer
	}
	n.depth++
}

func (n *Nesting[K]) Top() (K, bool) {
	if n.depth == 0 || !n.Tracked() {
		var none K
		return none, false
	}
	return n.closers[n.depth-1], true
}

func (n *Nesting[K]) Pop(closer K) bool {
	top, known := n.Top()
	if n.depth == 0 || (known && top != closer) {
		return false
	}
	n.depth--
	return true
}
