# config_manager/proactive_speak.py
"""空闲自动发言（主动发起话题）配置。

当客户端空闲一段时间后，后端会自动触发一次主动发言；
发言内容会结合最近对话上下文，并可选的通过网络搜索热点来生成话题。
"""

from pydantic import Field
from typing import List, ClassVar, Dict
from .i18n import I18nMixin, Description


class ProactiveSpeakConfig(I18nMixin):
    """空闲自动发言配置。

    所有字段都有默认值，保证向后兼容：已有用户的 characters/*.yaml
    与 conf.yaml 即使不配置该块，也能正常通过校验。
    """

    enabled: bool = Field(False, alias="enabled")
    idle_timeout_seconds: int = Field(120, alias="idle_timeout_seconds")
    interval_seconds: int = Field(300, alias="interval_seconds")
    use_web_search: bool = Field(True, alias="use_web_search")
    search_queries: List[str] = Field(
        default_factory=lambda: ["AI", "科技", "今日热点"],
        alias="search_queries",
    )
    search_max_results: int = Field(5, alias="search_max_results")
    prompt_key: str = Field("idle_speak_prompt", alias="prompt_key")

    DESCRIPTIONS: ClassVar[Dict[str, Description]] = {
        "enabled": Description(
            en="Whether to enable idle proactive speak (starting a topic when idle)",
            zh="是否启用空闲自动发言（空闲时主动发起话题）",
        ),
        "idle_timeout_seconds": Description(
            en="How many seconds of idleness trigger a proactive speak",
            zh="空闲多少秒后触发一次主动发言",
        ),
        "interval_seconds": Description(
            en="Minimum interval between two proactive speaks to avoid spamming",
            zh="两次主动发言之间的最小间隔，防止刷屏",
        ),
        "use_web_search": Description(
            en="Whether to use web search hot topics to generate a topic",
            zh="是否使用网络搜索热点来生成话题",
        ),
        "search_queries": Description(
            en="Candidate search keywords for fetching hot topics (rotated across triggers)",
            zh="用于获取热点的搜索关键词列表（多次触发时轮换使用）",
        ),
        "search_max_results": Description(
            en="Maximum number of search results to fetch per hot-topic search",
            zh="每次热点搜索最多获取的结果条数",
        ),
        "prompt_key": Description(
            en="Key in system_config.tool_prompts pointing to the proactive speak prompt file",
            zh="system_config.tool_prompts 中指向主动发言提示词文件的键名",
        ),
    }