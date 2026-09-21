package filter

import "testing"

func TestTrieInsertAndContains(t *testing.T) {
	trie := NewTrie()
	trie.Insert("赌博")
	trie.Insert("法轮功")

	if !trie.Contains("他沉迷赌博无法自拔") {
		t.Fatal("应命中词库词「赌博」")
	}
	if !trie.Contains("法轮功是邪教") {
		t.Fatal("应命中词库词「法轮功」")
	}
	if trie.Contains("今天天气不错") {
		t.Fatal("不应误命中")
	}
	if trie.Contains("赌") {
		t.Fatal("前缀不应误命中：词库没有单字「赌」")
	}
}

func TestTrieSubstringMatch(t *testing.T) {
	trie := NewTrie()
	trie.Insert("天气")
	// 命中位置不在文本开头
	if !trie.Contains("今天天气不错") {
		t.Fatal("应从任意起始位置匹配「天气」")
	}
}

func TestTrieMultiByteAndEmptyWord(t *testing.T) {
	trie := NewTrie()
	trie.Insert("") // 空词不应 panic
	trie.Insert("人民币")
	if !trie.Contains("我想用人民币") {
		t.Fatal("应命中多字节词「人民币」")
	}
	if trie.Contains("人民") {
		t.Fatal("不应误命中「人民」")
	}
}

func TestTrieEmptyTrie(t *testing.T) {
	trie := NewTrie()
	if trie.Contains("任何文本") {
		t.Fatal("空词库不应命中")
	}
}
