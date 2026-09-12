"""本地长期记忆模块（P1 最小实现）。

- ``MemoryStore``：SQLite 存储 + FTS5(trigram) 中文子串检索，标准库零依赖。
- ``extract_rules``：按 whitelist 规则从用户消息中抽取值得长期记忆的事实句。

默认关闭，由 ``agent_settings.basic_memory_agent.memory.enabled`` 开启。
存储文件 ``memory.db`` 已在仓库 .gitignore 中。
"""

import re
import sqlite3
import threading
import time
from typing import Dict, List, Optional, Tuple

from loguru import logger

# 记忆事实抽取规则：命中任一规则的用户句子即视为值得长期记忆
_EXTRACT_RULES: List[str] = [
    r"我(?:叫|是|的名字是|的名字叫)\S+",
    r"我(?:喜欢|爱|讨厌|不喜欢|想要|想|住在|来自|生日是|是)\S+",
    r"(?:我的|我)(?:生日|名字|职业|工作|城市|家)\s*(?:是|叫|在|有)\S+",
]

# 句子切分符：中文/英文句末标点
_SENTENCE_SPLIT_RE = re.compile(r"[。！？!?；;\n]+")

# 召回排序的时间衰减周期（秒）：7 天
_RECALL_DECAY_SECONDS = 7 * 24 * 3600


def extract_rules(text: str) -> List[Tuple[str, str, float]]:
    """按规则从用户消息中抽取事实句。

    返回 ``[(content, kind, importance)]``；未命中任何规则时返回空列表。
    """
    hits: List[Tuple[str, str, float]] = []
    for sentence in _SENTENCE_SPLIT_RE.split(text):
        sentence = sentence.strip()
        if not sentence:
            continue
        for pattern in _EXTRACT_RULES:
            if re.search(pattern, sentence):
                hits.append((sentence, "fact", 0.6))
                break
    return hits


class MemoryStore:
    """SQLite 长期记忆存储，线程安全（FastAPI 多线程/事件循环共用）。"""

    def __init__(
        self,
        db_path: str = "memory.db",
        max_recall: int = 5,
        max_context_chars: int = 1500,
        min_importance: float = 0.3,
    ):
        self.db_path = db_path
        self.max_recall = max_recall
        self.max_context_chars = max_context_chars
        self.min_importance = min_importance
        self._lock = threading.Lock()
        self._conn = sqlite3.connect(db_path, check_same_thread=False)
        self._fts_available = self._init_schema()
        logger.info(f"本地记忆存储已初始化: db={db_path}, fts5={self._fts_available}")

    def _init_schema(self) -> bool:
        """建表并返回 FTS5(trigram) 是否可用。"""
        with self._lock:
            self._conn.execute(
                """
                CREATE TABLE IF NOT EXISTS memories (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    user_id TEXT NOT NULL,
                    content TEXT NOT NULL,
                    kind TEXT NOT NULL DEFAULT 'fact',
                    importance REAL NOT NULL DEFAULT 0.5,
                    source TEXT NOT NULL DEFAULT '',
                    created_at REAL NOT NULL,
                    updated_at REAL NOT NULL,
                    last_recalled_at REAL,
                    recall_count INTEGER NOT NULL DEFAULT 0
                )
                """
            )
            self._conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_memories_user ON memories(user_id)"
            )
            try:
                self._conn.execute(
                    """
                    CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts
                    USING fts5(content, tokenize='trigram')
                    """
                )
                self._conn.commit()
                return True
            except sqlite3.OperationalError:
                # 目标 SQLite 不支持 trigram tokenizer：回退 LIKE 子串扫描
                logger.warning("FTS5 trigram 不可用，记忆检索回退为 LIKE 子串匹配")
                self._conn.commit()
                return False

    def add(
        self,
        user_id: str,
        content: str,
        kind: str = "fact",
        importance: float = 0.5,
        source: str = "",
    ) -> bool:
        """写入一条记忆；同用户同内容去重（保留更高 importance）。

        低于 ``min_importance`` 的内容直接丢弃。
        """
        content = content.strip()
        if not content or importance < self.min_importance:
            return False
        now = time.time()
        with self._lock:
            row = self._conn.execute(
                "SELECT id, importance FROM memories WHERE user_id=? AND content=?",
                (user_id, content),
            ).fetchone()
            if row is not None:
                self._conn.execute(
                    "UPDATE memories SET importance=MAX(importance, ?), updated_at=? WHERE id=?",
                    (importance, now, row[0]),
                )
            else:
                cursor = self._conn.execute(
                    """
                    INSERT INTO memories
                        (user_id, content, kind, importance, source, created_at, updated_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?)
                    """,
                    (user_id, content, kind, importance, source, now, now),
                )
                if self._fts_available:
                    self._conn.execute(
                        "INSERT INTO memories_fts (rowid, content) VALUES (?, ?)",
                        (cursor.lastrowid, content),
                    )
            self._conn.commit()
        return True

    def _recall_sql(self, now: float) -> str:
        """召回排序：importance × 时间衰减（越新越靠前）。"""
        return (
            "SELECT m.id, m.content, m.kind, m.importance "
            "FROM memories_fts f JOIN memories m ON m.id = f.rowid "
            "WHERE f.memories_fts MATCH ? AND m.user_id = ? AND m.importance >= ? "
            f"ORDER BY m.importance * (1.0 / (1.0 + (? - m.updated_at) / {_RECALL_DECAY_SECONDS})) DESC "
            "LIMIT ?"
        )

    def recall(
        self, user_id: str, query: str, k: Optional[int] = None
    ) -> List[Dict[str, object]]:
        """按查询词召回记忆，并按命中更新召回统计。

        检索质量受限于 FTS5 trigram（中文子串匹配）；不可用时回退 LIKE。
        """
        k = k or self.max_recall
        query = query.strip()
        if not query:
            return []
        now = time.time()
        rows: List[tuple] = []
        with self._lock:
            if self._fts_available:
                try:
                    # 双引号包裹为短语查询并转义内部引号
                    phrase = '"' + query.replace('"', '""') + '"'
                    rows = self._conn.execute(
                        self._recall_sql(now),
                        (phrase, user_id, self.min_importance, now, k),
                    ).fetchall()
                except sqlite3.OperationalError as e:
                    logger.warning(f"FTS 检索失败({e})，回退 LIKE 匹配")
                    self._fts_available = False
            if not rows:
                rows = self._conn.execute(
                    """
                    SELECT id, content, kind, importance FROM memories
                    WHERE user_id = ? AND importance >= ? AND content LIKE ?
                    ORDER BY importance * (1.0 / (1.0 + (? - updated_at) / ?)) DESC
                    LIMIT ?
                    """,
                    (
                        user_id,
                        self.min_importance,
                        f"%{query}%",
                        now,
                        _RECALL_DECAY_SECONDS,
                        k,
                    ),
                ).fetchall()
            if rows:
                self._conn.executemany(
                    "UPDATE memories SET last_recalled_at=?, recall_count=recall_count+1 WHERE id=?",
                    [(now, r[0]) for r in rows],
                )
                self._conn.commit()
        return [
            {"id": r[0], "content": r[1], "kind": r[2], "importance": r[3]}
            for r in rows
        ]

    def load_recent(
        self, user_id: str, k: Optional[int] = None
    ) -> List[Dict[str, object]]:
        """会话级加载：返回该用户高权重、较新的记忆（无查询词时使用）。"""
        k = k or self.max_recall
        with self._lock:
            rows = self._conn.execute(
                """
                SELECT id, content, kind, importance FROM memories
                WHERE user_id = ? AND importance >= ?
                ORDER BY importance DESC, updated_at DESC
                LIMIT ?
                """,
                (user_id, self.min_importance, k),
            ).fetchall()
        return [
            {"id": r[0], "content": r[1], "kind": r[2], "importance": r[3]}
            for r in rows
        ]

    def close(self) -> None:
        """关闭数据库连接。"""
        with self._lock:
            self._conn.close()
