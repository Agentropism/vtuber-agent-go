"""本地长期记忆模块测试：规则抽取、存储检索、agent 跨会话挂点。"""

import sqlite3

from open_llm_vtuber.memory import MemoryStore, extract_rules
from open_llm_vtuber.config_manager.agent import BasicMemoryAgentConfig
from open_llm_vtuber.agent.agents.basic_memory_agent import BasicMemoryAgent
from open_llm_vtuber.agent.input_types import BatchInput, TextData, TextSource


def test_memory_config_parses():
    """MemoryConfig 嵌套解析与默认关闭。"""
    default = BasicMemoryAgentConfig(llm_provider="ollama_llm")
    assert default.memory is None  # 未配置时记忆关闭

    cfg = BasicMemoryAgentConfig(
        llm_provider="ollama_llm",
        memory={"enabled": True, "db_path": "/tmp/m.db", "max_recall": 3},
    )
    assert cfg.memory is not None
    assert cfg.memory.enabled is True
    assert cfg.memory.db_path == "/tmp/m.db"
    assert cfg.memory.max_recall == 3
    assert cfg.memory.max_context_chars == 1500  # 未指定走默认


def test_extract_rules_hits_facts():
    """命中"我叫/我喜欢"规则的事实句应被抽取。"""
    hits = extract_rules("我叫小明，我喜欢吃冰淇淋")
    contents = [c for c, _, _ in hits]
    assert any("小明" in c for c in contents)
    assert any("冰淇淋" in c for c in contents)


def test_extract_rules_ignores_chitchat():
    """普通闲聊不应被抽取为长期记忆。"""
    assert extract_rules("今天天气不错啊") == []


def test_add_dedup_and_importance_max(tmp_path):
    """同用户同内容去重；importance 取 max；不同用户相互隔离。"""
    store = MemoryStore(db_path=str(tmp_path / "memory.db"))
    assert store.add("u1", "我叫小明", "fact", 0.6)
    store.add("u1", "我叫小明", "fact", 0.8)  # 重复写入，importance 更高
    store.add("u2", "我叫小明", "fact", 0.6)  # 不同用户，独立

    u1 = store.load_recent("u1", k=10)
    assert len(u1) == 1
    assert u1[0]["importance"] == 0.8
    assert len(store.load_recent("u2", k=10)) == 1


def test_add_drops_low_importance(tmp_path):
    """低于 min_importance 的记忆直接丢弃。"""
    store = MemoryStore(db_path=str(tmp_path / "memory.db"), min_importance=0.3)
    assert not store.add("u1", "无价值内容", "fact", 0.1)
    assert store.load_recent("u1", k=10) == []


def test_recall_chinese_substring(tmp_path):
    """中文子串检索可召回相关记忆，不相关查询不命中。"""
    store = MemoryStore(db_path=str(tmp_path / "memory.db"))
    store.add("u1", "我叫小明，我最喜欢草莓味冰淇淋", "fact", 0.6)
    store.add("u1", "我住在上海", "fact", 0.6)

    results = store.recall("u1", "冰淇淋")
    assert any("冰淇淋" in r["content"] for r in results)
    assert store.recall("u1", "量子力学") == []


def test_recall_updates_stats(tmp_path):
    """召回命中后更新 recall_count。"""
    store = MemoryStore(db_path=str(tmp_path / "memory.db"))
    store.add("u1", "我叫小明", "fact", 0.6)
    store.recall("u1", "小明")

    conn = sqlite3.connect(str(tmp_path / "memory.db"))
    count = conn.execute(
        "SELECT recall_count FROM memories WHERE user_id='u1'"
    ).fetchone()[0]
    conn.close()
    assert count >= 1


def test_agent_cross_session_memory(tmp_path):
    """同一角色跨会话：会话 A 写入事实，会话 B 新实例加载并注入记忆块。"""
    store = MemoryStore(db_path=str(tmp_path / "memory.db"))

    # 会话 A：set_memory_from_history 初始化身份，chat 收尾抽取写入
    agent_a = BasicMemoryAgent(
        llm=object(), system="你是助手", live2d_model=None, memory_store=store
    )
    agent_a.set_memory_from_history("conf1", "hist-a")
    agent_a._store_memory(
        BatchInput(texts=[TextData(source=TextSource.INPUT, content="我叫小明")])
    )
    assert store.recall("conf1", "小明")

    # 会话 B：新实例、新历史，同一 store —— 应加载长期记忆并注入
    agent_b = BasicMemoryAgent(
        llm=object(), system="你是助手", live2d_model=None, memory_store=store
    )
    agent_b.set_memory_from_history("conf1", "hist-b")
    assert any("小明" in m["content"] for m in agent_b._long_term_memories)

    messages = agent_b._to_messages(
        BatchInput(texts=[TextData(source=TextSource.INPUT, content="你好")])
    )
    assert messages[0]["role"] == "system"
    assert "<memory>" in messages[0]["content"]
    assert "小明" in messages[0]["content"]
