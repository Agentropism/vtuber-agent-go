"""Adapters for the onebot-gateway upload event protocol.

The wire format is specified in ``doc/UPLOAD_API.md``.  These models keep the
gateway payload intact while converting message events to the existing internal
``text-input`` command used by the conversation pipeline.
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


class UploadContentSegment(BaseModel):
    """One segment in an uploaded message's content_data array."""

    type: str
    text: str = ""


class UploadEvent(BaseModel):
    """One inbound event emitted by onebot-gateway."""

    model_config = ConfigDict(extra="allow")

    type: Literal["message", "notice"]
    platform_name: str
    channel_id: str = ""
    channel_name: str = ""
    channel_type: str = ""
    channel_avatar: str = ""
    user_id: str = ""
    user_name: str = ""
    user_avatar: str = ""
    message_id: str = ""
    sender_id: str = ""
    sender_name: str = ""
    sender_nickname: str = ""
    sender_avatar: str = ""
    content_data: list[UploadContentSegment] = Field(default_factory=list)
    content_text: str = ""
    is_tome: bool = False
    timestamp: int = 0
    is_self: bool = False
    ref_chat_key: str = ""
    ref_msg_id: str = ""
    ref_sender_id: str = ""

    @classmethod
    def is_upload_payload(cls, payload: dict) -> bool:
        """Return whether a payload claims to use the upload-event protocol."""
        return payload.get("type") in {"message", "notice"} and "platform_name" in payload

    @property
    def text(self) -> str:
        """Use content_text first, then fall back to text content segments."""
        if self.content_text:
            return self.content_text
        return "".join(
            segment.text for segment in self.content_data if segment.type == "text"
        )

    @property
    def trace_id(self) -> str:
        """Stable, log-safe correlation identifier for this upload event."""
        event_id = self.message_id or str(self.timestamp)
        return f"{self.platform_name}:{self.channel_id}:{event_id}"

    def to_text_input(self) -> dict:
        """Translate a message event into the established conversation command."""
        return {
            "type": "text-input",
            "text": self.text,
            "upload_event": self.model_dump(mode="json"),
        }

    def build_reply_action(self, text: str, reply_client_id: str) -> dict | None:
        """Build the standard onebot-gateway Action for a generated reply.

        UPLOAD_API currently defines QQ group and Bilibili live-room sources.
        Only QQ groups have a corresponding downstream action in onebot-gateway.
        """
        if self.platform_name != "qq" or self.channel_type != "group":
            return None

        group_id = self.channel_id.removeprefix("group_")
        if not group_id.isdigit():
            return None

        return {
            "action": "send_group_msg",
            "params": {"group_id": int(group_id), "message": text},
            "echo": {
                "upload_message_id": self.message_id,
                "upload_trace_id": self.trace_id,
                "proxy_client_id": reply_client_id,
            },
        }
