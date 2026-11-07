package redact

// acNoChild marks "no child at this byte" in a trieNode's children
// array — a map[byte]int here (as an earlier version used) measurably
// cost more than the regex matches it was meant to gate around, almost
// entirely in map hashing/bucket lookups on a per-byte hot path; a
// fixed 256-entry array indexed directly by the byte is the obvious
// fix once that showed up in a profile; see rules.go's Rule doc
// comment for the fuller history of what was tried before this.
const acNoChild = -1

type trieNode struct {
	children [256]int32
	fail     int32
	// rules is the set of rule indices whose keyword ends exactly at
	// this node, merged with every node reachable via its failure
	// link — so visiting a node during a scan directly gives every
	// rule that keyword (or any of its own suffixes already in the
	// automaton) activates, no extra walk needed per match.
	rules []int32
}

// keywordMatcher is a minimal Aho-Corasick automaton over every rule's
// (already-lowercased) Keywords: one pass over a string value finds
// every rule whose keyword is present in it, in time proportional to
// the string's length regardless of how many rules/keywords there are
// — the real version of the "Aho-Corasick + RE2" architecture
// redaction.md calls for. Hand-written rather than a dependency: this
// is a well-defined, compact algorithm (build a trie, add failure
// links), not something that needs its own library the way rules.go's
// doc comment explains gitleaks itself does for the pattern
// *definitions*.
type keywordMatcher struct {
	nodes []trieNode
}

// newKeywordMatcher builds the automaton from every rule's Keywords.
// A rule with no keywords contributes nothing here — its regex always
// runs, checked separately by the caller.
func newKeywordMatcher(allRules []Rule) *keywordMatcher {
	m := &keywordMatcher{nodes: []trieNode{newTrieNode()}}
	for ruleIdx, rule := range allRules {
		for _, kw := range rule.Keywords {
			m.insert(kw, int32(ruleIdx))
		}
	}
	m.buildFailureLinks()
	return m
}

func newTrieNode() trieNode {
	n := trieNode{}
	for i := range n.children {
		n.children[i] = acNoChild
	}
	return n
}

func (m *keywordMatcher) insert(keyword string, ruleIdx int32) {
	node := int32(0)
	for i := 0; i < len(keyword); i++ {
		b := keyword[i]
		next := m.nodes[node].children[b]
		if next == acNoChild {
			m.nodes = append(m.nodes, newTrieNode())
			next = int32(len(m.nodes) - 1) // #nosec G115 -- trie size is bounded by DefaultRules()' fixed keyword set, nowhere near int32
			m.nodes[node].children[b] = next
		}
		node = next
	}
	m.nodes[node].rules = append(m.nodes[node].rules, ruleIdx)
}

// buildFailureLinks is the standard Aho-Corasick BFS construction,
// merging each node's output set with its failure-link target's so a
// scan never needs to walk fail links itself — every node already
// carries everything active at that point.
func (m *keywordMatcher) buildFailureLinks() {
	var queue []int32
	for b := 0; b < 256; b++ {
		if child := m.nodes[0].children[b]; child != acNoChild {
			m.nodes[child].fail = 0
			queue = append(queue, child)
		}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		for b := 0; b < 256; b++ {
			child := m.nodes[node].children[b]
			if child == acNoChild {
				continue
			}
			queue = append(queue, child)

			f := m.nodes[node].fail
			for {
				if next := m.nodes[f].children[b]; next != acNoChild {
					m.nodes[child].fail = next
					break
				}
				if f == 0 {
					m.nodes[child].fail = 0
					break
				}
				f = m.nodes[f].fail
			}
			m.nodes[child].rules = append(m.nodes[child].rules, m.nodes[m.nodes[child].fail].rules...)
		}
	}
}

// matchedRules writes into dst (indexed the same way as the Rule slice
// newKeywordMatcher was built from, and already all-false — callers
// reuse one scratch buffer across calls rather than allocating a fresh
// one per string value scanned, see Redactor.matchedScratch) which
// rules had at least one of their keywords found anywhere in text —
// one linear pass regardless of how many rules/keywords there are.
func (m *keywordMatcher) matchedRules(text []byte, dst []bool) {
	node := int32(0)
	for _, b := range text {
		for {
			if next := m.nodes[node].children[b]; next != acNoChild {
				node = next
				break
			}
			if node == 0 {
				break
			}
			node = m.nodes[node].fail
		}
		for _, idx := range m.nodes[node].rules {
			dst[idx] = true
		}
	}
}
