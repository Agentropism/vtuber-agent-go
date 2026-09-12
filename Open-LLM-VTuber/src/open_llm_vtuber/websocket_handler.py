from typing import Dict, List, Optional, Callable, TypedDict
from fastapi import WebSocket, WebSocketDisconnect
import asyncio
import json
import time
from enum import Enum
import numpy as np
from loguru import logger
from prompts import prompt_loader

from .service_context import ServiceContext
from .chat_group import (
    ChatGroupManager,
    handle_group_operation,
    handle_client_disconnect,
    broadcast_to_group,
)
from .message_handler import message_handler
from .utils.stream_audio import prepare_audio_payload
from .chat_history_manager import (
    create_new_history,
    get_history,
    delete_history,
    get_history_list,
)
from .config_manager.utils import scan_config_alts_directory, scan_bg_directory
from .conversations.conversation_handler import (
    handle_conversation_trigger,
    handle_group_interrupt,
    handle_individual_interrupt,
)


class MessageType(Enum):
    """Enum for WebSocket message types"""

    GROUP = ["add-client-to-group", "remove-client-from-group"]
    HISTORY = [
        "fetch-history-list",
        "fetch-and-set-history",
        "create-new-history",
        "delete-history",
    ]
    CONVERSATION = ["mic-audio-end", "text-input", "ai-speak-signal"]
    CONFIG = ["fetch-configs", "switch-config"]
    CONTROL = ["interrupt-signal", "audio-play-start"]
    DATA = ["mic-audio-data"]


class WSMessage(TypedDict, total=False):
    """Type definition for WebSocket messages"""

    type: str
    action: Optional[str]
    text: Optional[str]
    audio: Optional[List[float]]
    images: Optional[List[str]]
    history_uid: Optional[str]
    file: Optional[str]
    display_text: Optional[dict]


class WebSocketHandler:
    """Handles WebSocket connections and message routing"""

    # 空闲自动发言相关的消息类型：收到这些消息时视为客户端活跃，刷新最后活跃时间
    ACTIVE_MESSAGE_TYPES = frozenset(
        {
            "text-input",
            "mic-audio-end",
            "mic-audio-data",
            "raw-audio-data",
            "ai-speak-signal",
            "interrupt-signal",
        }
    )

    # 空闲检查任务轮询间隔（秒）
    IDLE_CHECK_INTERVAL = 5.0

    def __init__(self, default_context_cache: ServiceContext):
        """Initialize the WebSocket handler with default context"""
        self.client_connections: Dict[str, WebSocket] = {}
        self.client_contexts: Dict[str, ServiceContext] = {}
        self.chat_group_manager = ChatGroupManager()
        self.current_conversation_tasks: Dict[str, Optional[asyncio.Task]] = {}
        self.default_context_cache = default_context_cache
        self.received_data_buffers: Dict[str, np.ndarray] = {}

        # 空闲自动发言状态：最后活跃时间 / 上次主动发言时间 / 搜索关键词轮换索引
        self.client_last_active: Dict[str, float] = {}
        self.client_last_idle_speak: Dict[str, float] = {}
        self.client_idle_speak_index: Dict[str, int] = {}
        self._idle_speak_task: Optional[asyncio.Task] = None

        # Message handlers mapping
        self._message_handlers = self._init_message_handlers()

    def _init_message_handlers(self) -> Dict[str, Callable]:
        """Initialize message type to handler mapping"""
        return {
            "add-client-to-group": self._handle_group_operation,
            "remove-client-from-group": self._handle_group_operation,
            "request-group-info": self._handle_group_info,
            "fetch-history-list": self._handle_history_list_request,
            "fetch-and-set-history": self._handle_fetch_history,
            "create-new-history": self._handle_create_history,
            "delete-history": self._handle_delete_history,
            "interrupt-signal": self._handle_interrupt,
            "mic-audio-data": self._handle_audio_data,
            "mic-audio-end": self._handle_conversation_trigger,
            "raw-audio-data": self._handle_raw_audio_data,
            "text-input": self._handle_conversation_trigger,
            "ai-speak-signal": self._handle_conversation_trigger,
            "fetch-configs": self._handle_fetch_configs,
            "switch-config": self._handle_config_switch,
            "fetch-backgrounds": self._handle_fetch_backgrounds,
            "audio-play-start": self._handle_audio_play_start,
            "request-init-config": self._handle_init_config_request,
            "heartbeat": self._handle_heartbeat,
        }

    async def handle_new_connection(
        self, websocket: WebSocket, client_uid: str
    ) -> None:
        """
        Handle new WebSocket connection setup

        Args:
            websocket: The WebSocket connection
            client_uid: Unique identifier for the client

        Raises:
            Exception: If initialization fails
        """
        try:
            session_service_context = await self._init_service_context(
                websocket.send_text, client_uid
            )

            await self._store_client_data(
                websocket, client_uid, session_service_context
            )

            await self._send_initial_messages(
                websocket, client_uid, session_service_context
            )

            # 首次连接时启动空闲自动发言检查任务（若尚未运行）
            if self._idle_speak_task is None or self._idle_speak_task.done():
                self._idle_speak_task = asyncio.create_task(
                    self._idle_speak_check_loop()
                )

            logger.info(f"Connection established for client {client_uid}")

        except Exception as e:
            logger.error(
                f"Failed to initialize connection for client {client_uid}: {e}"
            )
            await self._cleanup_failed_connection(client_uid)
            raise

    async def _store_client_data(
        self,
        websocket: WebSocket,
        client_uid: str,
        session_service_context: ServiceContext,
    ):
        """Store client data and initialize group status"""
        self.client_connections[client_uid] = websocket
        self.client_contexts[client_uid] = session_service_context
        self.received_data_buffers[client_uid] = np.array([])

        self.chat_group_manager.client_group_map[client_uid] = ""
        await self.send_group_update(websocket, client_uid)

    async def _send_initial_messages(
        self,
        websocket: WebSocket,
        client_uid: str,
        session_service_context: ServiceContext,
    ):
        """Send initial connection messages to the client"""
        await websocket.send_text(
            json.dumps({"type": "full-text", "text": "Connection established"})
        )

        await websocket.send_text(
            json.dumps(
                {
                    "type": "set-model-and-conf",
                    "model_info": session_service_context.live2d_model.model_info,
                    "conf_name": session_service_context.character_config.conf_name,
                    "conf_uid": session_service_context.character_config.conf_uid,
                    "client_uid": client_uid,
                }
            )
        )

        # Send initial group status
        await self.send_group_update(websocket, client_uid)

        # Start microphone
        await websocket.send_text(json.dumps({"type": "control", "text": "start-mic"}))

    async def _init_service_context(
        self, send_text: Callable, client_uid: str
    ) -> ServiceContext:
        """Initialize service context for a new session by cloning the default context"""
        session_service_context = ServiceContext()
        await session_service_context.load_cache(
            config=self.default_context_cache.config.model_copy(deep=True),
            system_config=self.default_context_cache.system_config.model_copy(
                deep=True
            ),
            character_config=self.default_context_cache.character_config.model_copy(
                deep=True
            ),
            live2d_model=self.default_context_cache.live2d_model,
            asr_engine=self.default_context_cache.asr_engine,
            tts_engine=self.default_context_cache.tts_engine,
            vad_engine=self.default_context_cache.vad_engine,
            agent_engine=self.default_context_cache.agent_engine,
            translate_engine=self.default_context_cache.translate_engine,
            mcp_server_registery=self.default_context_cache.mcp_server_registery,
            tool_adapter=self.default_context_cache.tool_adapter,
            send_text=send_text,
            client_uid=client_uid,
        )
        return session_service_context

    async def handle_websocket_communication(
        self, websocket: WebSocket, client_uid: str
    ) -> None:
        """
        Handle ongoing WebSocket communication

        Args:
            websocket: The WebSocket connection
            client_uid: Unique identifier for the client
        """
        try:
            while True:
                try:
                    data = await websocket.receive_json()
                    message_handler.handle_message(client_uid, data)
                    await self._route_message(websocket, client_uid, data)
                except WebSocketDisconnect:
                    raise
                except json.JSONDecodeError:
                    logger.error("Invalid JSON received")
                    continue
                except Exception as e:
                    logger.error(f"Error processing message: {e}")
                    await websocket.send_text(
                        json.dumps({"type": "error", "message": str(e)})
                    )
                    continue

        except WebSocketDisconnect:
            logger.info(f"Client {client_uid} disconnected")
            raise
        except Exception as e:
            logger.error(f"Fatal error in WebSocket communication: {e}")
            raise

    async def _route_message(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """
        Route incoming message to appropriate handler

        Args:
            websocket: The WebSocket connection
            client_uid: Client identifier
            data: Message data
        """
        msg_type = data.get("type")
        if not msg_type:
            logger.warning("Message received without type")
            return

        # 收到活跃消息时刷新该客户端的最后活跃时间（用于空闲自动发言判断）
        if msg_type in self.ACTIVE_MESSAGE_TYPES:
            self.client_last_active[client_uid] = time.monotonic()

        handler = self._message_handlers.get(msg_type)
        if handler:
            await handler(websocket, client_uid, data)
        else:
            if msg_type != "frontend-playback-complete":
                logger.warning(f"Unknown message type: {msg_type}")

    async def _handle_group_operation(
        self, websocket: WebSocket, client_uid: str, data: dict
    ) -> None:
        """Handle group-related operations"""
        operation = data.get("type")
        target_uid = data.get(
            "invitee_uid" if operation == "add-client-to-group" else "target_uid"
        )

        await handle_group_operation(
            operation=operation,
            client_uid=client_uid,
            target_uid=target_uid,
            chat_group_manager=self.chat_group_manager,
            client_connections=self.client_connections,
            send_group_update=self.send_group_update,
        )

    async def handle_disconnect(self, client_uid: str) -> None:
        """Handle client disconnection"""
        group = self.chat_group_manager.get_client_group(client_uid)
        if group:
            await handle_group_interrupt(
                group_id=group.group_id,
                heard_response="",
                current_conversation_tasks=self.current_conversation_tasks,
                chat_group_manager=self.chat_group_manager,
                client_contexts=self.client_contexts,
                broadcast_to_group=self.broadcast_to_group,
            )

        await handle_client_disconnect(
            client_uid=client_uid,
            chat_group_manager=self.chat_group_manager,
            client_connections=self.client_connections,
            send_group_update=self.send_group_update,
        )

        # Clean up other client data
        self.client_connections.pop(client_uid, None)
        self.client_contexts.pop(client_uid, None)
        self.received_data_buffers.pop(client_uid, None)
        if client_uid in self.current_conversation_tasks:
            task = self.current_conversation_tasks[client_uid]
            if task and not task.done():
                task.cancel()
            self.current_conversation_tasks.pop(client_uid, None)

        # 清理空闲自动发言状态
        self.client_last_active.pop(client_uid, None)
        self.client_last_idle_speak.pop(client_uid, None)
        self.client_idle_speak_index.pop(client_uid, None)
        # 最后一个客户端断开时取消后台检查任务，避免悬挂
        if not self.client_connections and self._idle_speak_task:
            if not self._idle_speak_task.done():
                self._idle_speak_task.cancel()
            self._idle_speak_task = None

        # Call context close to clean up resources (e.g., MCPClient)
        context = self.client_contexts.get(client_uid)
        if context:
            await context.close()

        logger.info(f"Client {client_uid} disconnected")
        message_handler.cleanup_client(client_uid)

    async def _cleanup_failed_connection(self, client_uid: str) -> None:
        """Clean up failed connection data"""
        self.client_connections.pop(client_uid, None)
        self.client_contexts.pop(client_uid, None)
        self.received_data_buffers.pop(client_uid, None)
        self.chat_group_manager.client_group_map.pop(client_uid, None)

        if client_uid in self.current_conversation_tasks:
            task = self.current_conversation_tasks[client_uid]
            if task and not task.done():
                task.cancel()
            self.current_conversation_tasks.pop(client_uid, None)

        self.client_last_active.pop(client_uid, None)
        self.client_last_idle_speak.pop(client_uid, None)
        self.client_idle_speak_index.pop(client_uid, None)

        message_handler.cleanup_client(client_uid)

    async def broadcast_to_group(
        self, group_members: list[str], message: dict, exclude_uid: str = None
    ) -> None:
        """Broadcasts a message to group members"""
        await broadcast_to_group(
            group_members=group_members,
            message=message,
            client_connections=self.client_connections,
            exclude_uid=exclude_uid,
        )

    async def send_group_update(self, websocket: WebSocket, client_uid: str):
        """Sends group information to a client"""
        group = self.chat_group_manager.get_client_group(client_uid)
        if group:
            current_members = self.chat_group_manager.get_group_members(client_uid)
            await websocket.send_text(
                json.dumps(
                    {
                        "type": "group-update",
                        "members": current_members,
                        "is_owner": group.owner_uid == client_uid,
                    }
                )
            )
        else:
            await websocket.send_text(
                json.dumps(
                    {
                        "type": "group-update",
                        "members": [],
                        "is_owner": False,
                    }
                )
            )

    async def _handle_interrupt(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle conversation interruption"""
        heard_response = data.get("text", "")
        context = self.client_contexts[client_uid]
        group = self.chat_group_manager.get_client_group(client_uid)

        if group and len(group.members) > 1:
            await handle_group_interrupt(
                group_id=group.group_id,
                heard_response=heard_response,
                current_conversation_tasks=self.current_conversation_tasks,
                chat_group_manager=self.chat_group_manager,
                client_contexts=self.client_contexts,
                broadcast_to_group=self.broadcast_to_group,
            )
        else:
            await handle_individual_interrupt(
                client_uid=client_uid,
                current_conversation_tasks=self.current_conversation_tasks,
                context=context,
                heard_response=heard_response,
            )

    async def _handle_history_list_request(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle request for chat history list"""
        context = self.client_contexts[client_uid]
        histories = get_history_list(context.character_config.conf_uid)
        await websocket.send_text(
            json.dumps({"type": "history-list", "histories": histories})
        )

    async def _handle_fetch_history(
        self, websocket: WebSocket, client_uid: str, data: dict
    ):
        """Handle fetching and setting specific chat history"""
        history_uid = data.get("history_uid")
        if not history_uid:
            return

        context = self.client_contexts[client_uid]
        # Update history_uid in service context
        context.history_uid = history_uid
        context.agent_engine.set_memory_from_history(
            conf_uid=context.character_config.conf_uid,
            history_uid=history_uid,
        )

        messages = [
            msg
            for msg in get_history(
                context.character_config.conf_uid,
                history_uid,
            )
            if msg["role"] != "system"
        ]
        await websocket.send_text(
            json.dumps({"type": "history-data", "messages": messages})
        )

    async def _handle_create_history(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle creation of new chat history"""
        context = self.client_contexts[client_uid]
        history_uid = create_new_history(context.character_config.conf_uid)
        if history_uid:
            context.history_uid = history_uid
            context.agent_engine.set_memory_from_history(
                conf_uid=context.character_config.conf_uid,
                history_uid=history_uid,
            )
            await websocket.send_text(
                json.dumps(
                    {
                        "type": "new-history-created",
                        "history_uid": history_uid,
                    }
                )
            )

    async def _handle_delete_history(
        self, websocket: WebSocket, client_uid: str, data: dict
    ):
        """Handle deletion of chat history"""
        history_uid = data.get("history_uid")
        if not history_uid:
            return

        context = self.client_contexts[client_uid]
        success = delete_history(
            context.character_config.conf_uid,
            history_uid,
        )
        await websocket.send_text(
            json.dumps(
                {
                    "type": "history-deleted",
                    "success": success,
                    "history_uid": history_uid,
                }
            )
        )
        if history_uid == context.history_uid:
            context.history_uid = None

    async def _handle_audio_data(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle incoming audio data"""
        audio_data = data.get("audio", [])
        if audio_data:
            self.received_data_buffers[client_uid] = np.append(
                self.received_data_buffers[client_uid],
                np.array(audio_data, dtype=np.float32),
            )

    async def _handle_raw_audio_data(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle incoming raw audio data for VAD processing"""
        context = self.client_contexts[client_uid]
        chunk = data.get("audio", [])
        if chunk:
            for audio_bytes in context.vad_engine.detect_speech(chunk):
                if audio_bytes == b"<|PAUSE|>":
                    await websocket.send_text(
                        json.dumps({"type": "control", "text": "interrupt"})
                    )
                elif audio_bytes == b"<|RESUME|>":
                    pass
                elif len(audio_bytes) > 1024:
                    # Detected audio activity (voice)
                    self.received_data_buffers[client_uid] = np.append(
                        self.received_data_buffers[client_uid],
                        np.frombuffer(audio_bytes, dtype=np.int16).astype(np.float32),
                    )
                    await websocket.send_text(
                        json.dumps({"type": "control", "text": "mic-audio-end"})
                    )

    async def _handle_conversation_trigger(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle triggers that start a conversation"""
        await handle_conversation_trigger(
            msg_type=data.get("type", ""),
            data=data,
            client_uid=client_uid,
            context=self.client_contexts[client_uid],
            websocket=websocket,
            client_contexts=self.client_contexts,
            client_connections=self.client_connections,
            chat_group_manager=self.chat_group_manager,
            received_data_buffers=self.received_data_buffers,
            current_conversation_tasks=self.current_conversation_tasks,
            broadcast_to_group=self.broadcast_to_group,
        )

    async def _handle_fetch_configs(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle fetching available configurations"""
        context = self.client_contexts[client_uid]
        config_files = scan_config_alts_directory(context.system_config.config_alts_dir)
        await websocket.send_text(
            json.dumps({"type": "config-files", "configs": config_files})
        )

    async def _handle_config_switch(
        self, websocket: WebSocket, client_uid: str, data: dict
    ):
        """Handle switching to a different configuration"""
        config_file_name = data.get("file")
        if config_file_name:
            context = self.client_contexts[client_uid]
            await context.handle_config_switch(websocket, config_file_name)

    async def _handle_fetch_backgrounds(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle fetching available background images"""
        bg_files = scan_bg_directory()
        await websocket.send_text(
            json.dumps({"type": "background-files", "files": bg_files})
        )

    async def _handle_audio_play_start(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """
        Handle audio playback start notification
        """
        group_members = self.chat_group_manager.get_group_members(client_uid)
        if len(group_members) > 1:
            display_text = data.get("display_text")
            if display_text:
                silent_payload = prepare_audio_payload(
                    audio_path=None,
                    display_text=display_text,
                    actions=None,
                    forwarded=True,
                )
                await self.broadcast_to_group(
                    group_members, silent_payload, exclude_uid=client_uid
                )

    async def _handle_group_info(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle group info request"""
        await self.send_group_update(websocket, client_uid)

    async def _handle_init_config_request(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle request for initialization configuration"""
        context = self.client_contexts.get(client_uid)
        if not context:
            context = self.default_context_cache

        await websocket.send_text(
            json.dumps(
                {
                    "type": "set-model-and-conf",
                    "model_info": context.live2d_model.model_info,
                    "conf_name": context.character_config.conf_name,
                    "conf_uid": context.character_config.conf_uid,
                    "client_uid": client_uid,
                }
            )
        )

    async def _handle_heartbeat(
        self, websocket: WebSocket, client_uid: str, data: WSMessage
    ) -> None:
        """Handle heartbeat messages from clients"""
        try:
            await websocket.send_json({"type": "heartbeat-ack"})
        except Exception as e:
            logger.error(f"Error sending heartbeat acknowledgment: {e}")

    async def inject_upload_message(self, data: dict) -> bool:
        """
        将来自网关的 upload 消息注入到第一个活跃的浏览器客户端 session 中。
        返回是否成功注入。
        """
        if not self.client_connections:
            logger.warning("inject_upload_message: 没有活跃的客户端连接")
            return False

        client_uid = next(iter(self.client_connections))
        websocket = self.client_connections[client_uid]
        context = self.client_contexts.get(client_uid)
        if not context:
            logger.warning(f"inject_upload_message: 客户端 {client_uid} 没有 context")
            return False

        msg = {
            "type": "text-input",
            "text": data.get("text", ""),
            "upload_event": data.get("upload_event"),
            "upload_reply_client_id": data.get("upload_reply_client_id", ""),
        }

        await handle_conversation_trigger(
            msg_type="text-input",
            data=msg,
            client_uid=client_uid,
            context=context,
            websocket=websocket,
            client_contexts=self.client_contexts,
            client_connections=self.client_connections,
            chat_group_manager=self.chat_group_manager,
            received_data_buffers=self.received_data_buffers,
            current_conversation_tasks=self.current_conversation_tasks,
            broadcast_to_group=self.broadcast_to_group,
        )
        return True

    # ============ 空闲自动发言（主动发起话题） ============

    async def _idle_speak_check_loop(self) -> None:
        """后台任务：周期性检查各客户端是否满足空闲自动发言条件并触发发言。"""
        logger.info("空闲自动发言检查任务已启动")
        try:
            while True:
                await asyncio.sleep(self.IDLE_CHECK_INTERVAL)
                await self._check_idle_speak_clients()
        except asyncio.CancelledError:
            logger.info("空闲自动发言检查任务已取消")
            raise

    async def _check_idle_speak_clients(self) -> None:
        """检查所有已连接客户端，对满足条件的客户端触发一次主动发言。"""
        now = time.monotonic()
        for client_uid in list(self.client_connections.keys()):
            context = self.client_contexts.get(client_uid)
            websocket = self.client_connections.get(client_uid)
            if not context or not websocket:
                continue

            cfg = context.character_config.proactive_speak_config
            if not cfg.enabled:
                continue

            # 空闲时长不足
            last_active = self.client_last_active.get(client_uid, now)
            if now - last_active < cfg.idle_timeout_seconds:
                continue

            # 距上次主动发言间隔不足（防止刷屏）
            last_speak = self.client_last_idle_speak.get(client_uid, 0.0)
            if now - last_speak < cfg.interval_seconds:
                continue

            # 不打断进行中的对话
            task = self.current_conversation_tasks.get(client_uid)
            if task and not task.done():
                continue

            # 群聊中不主动发起，避免干扰群组对话
            group = self.chat_group_manager.get_client_group(client_uid)
            if group and len(group.members) > 1:
                continue

            logger.info(f"客户端 {client_uid} 空闲超时，触发主动发言")
            try:
                await self._trigger_idle_speak(client_uid, websocket, context)
                self.client_last_idle_speak[client_uid] = now
            except Exception as e:
                logger.error(f"客户端 {client_uid} 触发主动发言失败：{e}")

    async def _trigger_idle_speak(
        self, client_uid: str, websocket: WebSocket, context: ServiceContext
    ) -> None:
        """构造空闲主动发言的内容（提示词 + 上下文 + 可选热点）并触发对话。"""
        cfg = context.character_config.proactive_speak_config

        # 1. 加载主动发言提示词（配置的 prompt_key 指向 tool_prompts 文件名）
        prompt_text = self._load_idle_speak_prompt(context, cfg.prompt_key)

        # 2. 最近对话上下文
        recent_context = self._get_recent_context(context)

        # 3. 网络搜索热点（可选，失败时优雅降级）
        hot_topics = ""
        if cfg.use_web_search:
            hot_topics = await self._fetch_hot_topics(client_uid, context, cfg)

        # 4. 组装 user_input：提示词 + 最近上下文 + 可选热点，交给 LLM 生成话题
        parts = [prompt_text]
        if recent_context:
            parts.append(f"<recent_context>\n{recent_context}\n</recent_context>")
        if hot_topics:
            parts.append(f"<hot_topics>\n{hot_topics}\n</hot_topics>")
        user_input = "\n\n".join(parts)

        msg = {"type": "ai-speak-signal", "text": user_input}

        await handle_conversation_trigger(
            msg_type="ai-speak-signal",
            data=msg,
            client_uid=client_uid,
            context=context,
            websocket=websocket,
            client_contexts=self.client_contexts,
            client_connections=self.client_connections,
            chat_group_manager=self.chat_group_manager,
            received_data_buffers=self.received_data_buffers,
            current_conversation_tasks=self.current_conversation_tasks,
            broadcast_to_group=self.broadcast_to_group,
        )

    def _load_idle_speak_prompt(self, context: ServiceContext, prompt_key: str) -> str:
        """加载主动发言提示词：优先用配置的 prompt_key，缺失时回退到 proactive_speak_prompt。"""
        default_text = "Please say something that would be engaging and appropriate for the current context."
        try:
            prompt_file = context.system_config.tool_prompts.get(prompt_key)
            if not prompt_file:
                prompt_file = context.system_config.tool_prompts.get(
                    "proactive_speak_prompt"
                )
            if prompt_file:
                return prompt_loader.load_util(prompt_file)
            logger.warning(
                f"主动发言提示词 {prompt_key} 未配置，使用默认提示词"
            )
            return default_text
        except Exception as e:
            logger.error(f"加载主动发言提示词失败：{e}")
            return default_text

    def _get_recent_context(
        self, context: ServiceContext, max_messages: int = 6
    ) -> str:
        """读取最近的对话历史，拼接成给 LLM 的上下文文本。"""
        history_uid = context.history_uid
        conf_uid = context.character_config.conf_uid
        if not history_uid:
            return ""
        try:
            history = get_history(conf_uid, history_uid)
            recent = history[-max_messages:]
            lines = []
            for msg in recent:
                role = msg.get("role")
                content = msg.get("content", "")
                role_label = "用户" if role == "human" else ("AI" if role == "ai" else str(role))
                lines.append(f"{role_label}：{content}")
            return "\n".join(lines)
        except Exception as e:
            logger.warning(f"获取最近对话上下文失败：{e}")
            return ""

    def _pick_search_query(self, client_uid: str, cfg) -> str:
        """从配置的搜索关键词列表中轮换选取一个查询词。"""
        queries = [q for q in cfg.search_queries if q]
        if not queries:
            return "今日热点新闻"
        index = self.client_idle_speak_index.get(client_uid, 0)
        self.client_idle_speak_index[client_uid] = (index + 1) % len(queries)
        return queries[index]

    async def _fetch_hot_topics(
        self, client_uid: str, context: ServiceContext, cfg
    ) -> str:
        """通过网络搜索（ddg-search）获取热点摘要；MCP 不可用或失败时优雅降级为空字符串。"""
        mcp_client = context.mcp_client
        enabled_servers = (
            context.character_config.agent_config.agent_settings.basic_memory_agent.mcp_enabled_servers
            or []
        )
        if not mcp_client or "ddg-search" not in enabled_servers:
            logger.debug("MCP 或 ddg-search 不可用，跳过热点搜索")
            return ""

        query = self._pick_search_query(client_uid, cfg)
        try:
            # 确认 ddg-search 提供 web_search 工具
            tools = await mcp_client.list_tools("ddg-search")
            tool_names = {t.name for t in tools}
            if "web_search" not in tool_names:
                logger.warning("ddg-search 未提供 web_search 工具，跳过热点搜索")
                return ""

            result = await asyncio.wait_for(
                mcp_client.call_tool(
                    "ddg-search",
                    "web_search",
                    {"query": query, "max_results": cfg.search_max_results},
                ),
                timeout=15,
            )

            texts = [
                item["text"]
                for item in result.get("content_items", [])
                if item.get("text")
            ]
            hot = "\n".join(texts)
            if hot:
                logger.info(f"网络搜索热点获取成功，查询词：{query}")
            return hot
        except asyncio.TimeoutError:
            logger.warning("网络搜索热点超时，降级为仅使用对话上下文")
        except Exception as e:
            logger.warning(f"网络搜索热点失败：{e}，降级为仅使用对话上下文")
        return ""
