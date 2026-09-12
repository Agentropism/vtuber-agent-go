import uuid
from fastapi import WebSocket
from loguru import logger
from pydantic import ValidationError
from starlette.websockets import WebSocketDisconnect

from .upload_event import UploadEvent


class ProxyHandler:
    """
    简单的网关桥接：接收 onebot-gateway 的 upload 事件，
    直接注入到 WebSocketHandler 的活跃浏览器客户端 session 中。
    不再维护独立的内部 /client-ws 连接。
    """

    def __init__(self, ws_handler):
        self.ws_handler = ws_handler
        self.clients: dict[str, WebSocket] = {}

    async def handle_client_connection(self, websocket: WebSocket):
        await websocket.accept()
        client_id = str(uuid.uuid4())
        self.clients[client_id] = websocket
        logger.info(f"网关客户端 {client_id} 已连接 proxy-ws，当前客户端数: {len(self.clients)}")

        try:
            while True:
                message = await websocket.receive_json()

                if not UploadEvent.is_upload_payload(message):
                    continue

                try:
                    upload_event = UploadEvent.model_validate(message)
                except ValidationError as e:
                    logger.warning(f"无效的 upload 事件已忽略: {e}")
                    continue

                if upload_event.type == "notice":
                    logger.info(
                        "[upload:{}] 收到通知 (text_length={})",
                        upload_event.trace_id,
                        len(upload_event.text),
                    )
                    continue

                if upload_event.is_self:
                    continue

                if not upload_event.text:
                    logger.warning("[upload:{}] 消息无文本，已忽略", upload_event.trace_id)
                    continue

                logger.info(
                    "[upload:{}] 接受来自 {} 的消息，注入到活跃 session (text_length={})",
                    upload_event.trace_id,
                    upload_event.sender_id or upload_event.user_id,
                    len(upload_event.text),
                )

                inject_data = {
                    "text": upload_event.text,
                    "upload_event": upload_event.model_dump(mode="json"),
                    "upload_reply_client_id": client_id,
                }

                ok = await self.ws_handler.inject_upload_message(inject_data)
                if not ok:
                    logger.warning("[upload:{}] 注入失败：无活跃客户端", upload_event.trace_id)

        except WebSocketDisconnect:
            pass
        except Exception as e:
            logger.error(f"proxy 客户端 {client_id} 异常: {e}")
        finally:
            self.clients.pop(client_id, None)
            logger.info(f"网关客户端 {client_id} 已断开，剩余客户端: {len(self.clients)}")
