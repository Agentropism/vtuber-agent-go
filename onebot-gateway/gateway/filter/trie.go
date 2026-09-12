package filter

// trieNode 前缀树节点，children 以 rune 为键，支持中文等多字节字符。
type trieNode struct {
	children map[rune]*trieNode
	terminal bool // 是否为某个敏感词的结尾
}

// Trie 基于 rune 的前缀树，用于敏感词匹配。
type Trie struct {
	root *trieNode
}

// NewTrie 创建空前缀树。
func NewTrie() *Trie {
	return &Trie{root: &trieNode{children: make(map[rune]*trieNode)}}
}

// Insert 插入一个敏感词，空词直接忽略。
func (t *Trie) Insert(word string) {
	node := t.root
	for _, r := range word {
		child, ok := node.children[r]
		if !ok {
			child = &trieNode{children: make(map[rune]*trieNode)}
			node.children[r] = child
		}
		node = child
	}
	node.terminal = true
}

// Contains 判断文本是否包含词库中的任意敏感词。
// 从文本的每个起始位置做一次前缀匹配，任一位置命中即返回 true。
// 词库规模较小时该实现足够；如需更高吞吐可后续升级为 AC 自动机。
func (t *Trie) Contains(text string) bool {
	runes := []rune(text)
	for i := range runes {
		node := t.root
		for j := i; j < len(runes); j++ {
			child, ok := node.children[runes[j]]
			if !ok {
				break
			}
			node = child
			if node.terminal {
				return true
			}
		}
	}
	return false
}
