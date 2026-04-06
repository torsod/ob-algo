package main

// ---------------------------------------------------------------------------
// Red-black tree keyed on Price, storing *PriceLevel values.
// Maintains sorted order on every insert/delete so the book can be
// queried at any time without rebuilding a sorted index.
// ---------------------------------------------------------------------------

type color bool

const (
	red   color = true
	black color = false
)

type rbNode struct {
	key    Price
	value  *PriceLevel
	color  color
	left   *rbNode
	right  *rbNode
	parent *rbNode
}

type RBTree struct {
	root *rbNode
	size int
}

func NewRBTree() *RBTree {
	return &RBTree{}
}

func (t *RBTree) Size() int { return t.size }

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

func (t *RBTree) Get(key Price) *PriceLevel {
	n := t.find(key)
	if n == nil {
		return nil
	}
	return n.value
}

func (t *RBTree) find(key Price) *rbNode {
	n := t.root
	for n != nil {
		if key < n.key {
			n = n.left
		} else if key > n.key {
			n = n.right
		} else {
			return n
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Max — best bid (highest price)
// ---------------------------------------------------------------------------

func (t *RBTree) Max() *PriceLevel {
	n := t.root
	if n == nil {
		return nil
	}
	for n.right != nil {
		n = n.right
	}
	return n.value
}

// ---------------------------------------------------------------------------
// Insert
// ---------------------------------------------------------------------------

func (t *RBTree) Insert(key Price, value *PriceLevel) {
	node := &rbNode{key: key, value: value, color: red}

	if t.root == nil {
		node.color = black
		t.root = node
		t.size++
		return
	}

	// BST insert
	n := t.root
	for {
		if key < n.key {
			if n.left == nil {
				n.left = node
				node.parent = n
				break
			}
			n = n.left
		} else if key > n.key {
			if n.right == nil {
				n.right = node
				node.parent = n
				break
			}
			n = n.right
		} else {
			// Key exists — update value
			n.value = value
			return
		}
	}

	t.size++
	t.insertFixup(node)
}

func (t *RBTree) insertFixup(n *rbNode) {
	for n != t.root && n.parent.color == red {
		if n.parent == n.parent.parent.left {
			uncle := n.parent.parent.right
			if uncle != nil && uncle.color == red {
				n.parent.color = black
				uncle.color = black
				n.parent.parent.color = red
				n = n.parent.parent
			} else {
				if n == n.parent.right {
					n = n.parent
					t.rotateLeft(n)
				}
				n.parent.color = black
				n.parent.parent.color = red
				t.rotateRight(n.parent.parent)
			}
		} else {
			uncle := n.parent.parent.left
			if uncle != nil && uncle.color == red {
				n.parent.color = black
				uncle.color = black
				n.parent.parent.color = red
				n = n.parent.parent
			} else {
				if n == n.parent.left {
					n = n.parent
					t.rotateRight(n)
				}
				n.parent.color = black
				n.parent.parent.color = red
				t.rotateLeft(n.parent.parent)
			}
		}
	}
	t.root.color = black
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func (t *RBTree) Delete(key Price) {
	n := t.find(key)
	if n == nil {
		return
	}
	t.deleteNode(n)
	t.size--
}

func (t *RBTree) deleteNode(n *rbNode) {
	var child, parent *rbNode
	nodeColor := n.color

	if n.left != nil && n.right != nil {
		// Find in-order successor
		succ := n.right
		for succ.left != nil {
			succ = succ.left
		}
		n.key = succ.key
		n.value = succ.value
		n = succ
		nodeColor = n.color
	}

	if n.left != nil {
		child = n.left
	} else {
		child = n.right
	}
	parent = n.parent

	if child != nil {
		child.parent = parent
	}

	if parent == nil {
		t.root = child
	} else if n == parent.left {
		parent.left = child
	} else {
		parent.right = child
	}

	if nodeColor == black {
		t.deleteFixup(child, parent)
	}
}

func (t *RBTree) deleteFixup(n *rbNode, parent *rbNode) {
	for n != t.root && (n == nil || n.color == black) {
		if n == parent.left {
			sibling := parent.right
			if sibling != nil && sibling.color == red {
				sibling.color = black
				parent.color = red
				t.rotateLeft(parent)
				sibling = parent.right
			}
			if (sibling.left == nil || sibling.left.color == black) &&
				(sibling.right == nil || sibling.right.color == black) {
				sibling.color = red
				n = parent
				parent = n.parent
			} else {
				if sibling.right == nil || sibling.right.color == black {
					if sibling.left != nil {
						sibling.left.color = black
					}
					sibling.color = red
					t.rotateRight(sibling)
					sibling = parent.right
				}
				sibling.color = parent.color
				parent.color = black
				if sibling.right != nil {
					sibling.right.color = black
				}
				t.rotateLeft(parent)
				n = t.root
			}
		} else {
			sibling := parent.left
			if sibling != nil && sibling.color == red {
				sibling.color = black
				parent.color = red
				t.rotateRight(parent)
				sibling = parent.left
			}
			if (sibling.left == nil || sibling.left.color == black) &&
				(sibling.right == nil || sibling.right.color == black) {
				sibling.color = red
				n = parent
				parent = n.parent
			} else {
				if sibling.left == nil || sibling.left.color == black {
					if sibling.right != nil {
						sibling.right.color = black
					}
					sibling.color = red
					t.rotateLeft(sibling)
					sibling = parent.left
				}
				sibling.color = parent.color
				parent.color = black
				if sibling.left != nil {
					sibling.left.color = black
				}
				t.rotateRight(parent)
				n = t.root
			}
		}
	}
	if n != nil {
		n.color = black
	}
}

// ---------------------------------------------------------------------------
// Rotations
// ---------------------------------------------------------------------------

func (t *RBTree) rotateLeft(n *rbNode) {
	r := n.right
	n.right = r.left
	if r.left != nil {
		r.left.parent = n
	}
	r.parent = n.parent
	if n.parent == nil {
		t.root = r
	} else if n == n.parent.left {
		n.parent.left = r
	} else {
		n.parent.right = r
	}
	r.left = n
	n.parent = r
}

func (t *RBTree) rotateRight(n *rbNode) {
	l := n.left
	n.left = l.right
	if l.right != nil {
		l.right.parent = n
	}
	l.parent = n.parent
	if n.parent == nil {
		t.root = l
	} else if n == n.parent.right {
		n.parent.right = l
	} else {
		n.parent.left = l
	}
	l.right = n
	n.parent = l
}

// ---------------------------------------------------------------------------
// Reverse in-order traversal (descending) — used for book display and sweeps
// ---------------------------------------------------------------------------

func (t *RBTree) DescendingDo(fn func(*PriceLevel)) {
	t.reverseInOrder(t.root, fn)
}

func (t *RBTree) reverseInOrder(n *rbNode, fn func(*PriceLevel)) {
	if n == nil {
		return
	}
	t.reverseInOrder(n.right, fn)
	fn(n.value)
	t.reverseInOrder(n.left, fn)
}
