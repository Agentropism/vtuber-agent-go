#!/usr/bin/env python3
"""端到端冒烟用的假外部服务：一个 OpenAI 兼容的流式 LLM，一个 OpenAI 兼容的 TTS。

只用标准库，方便在没有额外依赖的机器上直接跑。收到什么请求就写进对应的
日志文件，便于断言「网关确实按预期调用了外部服务」。

假 LLM 按请求内容分三种行为，用来覆盖三条不同的链路：

  1. 请求里已经带了工具结果 → 返回最终答复（工具调用循环的第二轮）；
  2. 用户消息里含「记得」→ 返回一个 memory_search 的工具调用（第一轮）；
  3. 其余 → 正常流式回复，分成三段吐出来，用来验证逐句入队。
"""

import json
import math
import os
import struct
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

WORK_DIR = os.environ.get("SMOKE_WORK", "/tmp/syagent-smoke")
LLM_PORT = int(os.environ.get("SMOKE_LLM_PORT", "19401"))
TTS_PORT = int(os.environ.get("SMOKE_TTS_PORT", "19402"))

# 普通回复：分三片，验证流式切句
REPLY_CHUNKS = ["你好呀，", "我是米粒。", "今天也要开心哦。"]
# 工具调用后的最终答复
TOOL_REPLY = "我记得你们刚聊过游戏。"
# 工具调用的参数（分两片下发，验证参数分片累积）
TOOL_QUERY_CHUNKS = ['{"query":', '"游戏"}']
TOOL_NAME = "memory_search"

TTS_SECONDS = 0.6
TTS_SAMPLE_RATE = 24000


def log_line(name, text):
    with open(os.path.join(WORK_DIR, name), "a", encoding="utf-8") as handle:
        handle.write(text + "\n")


def decide(payload):
    """按请求内容决定这次返回什么。

    返回 ("tool", None) 或 ("text", 文本)。
    """
    messages = payload.get("messages", [])

    if any(message.get("role") == "tool" for message in messages):
        return "text", TOOL_REPLY

    last_user = ""
    for message in reversed(messages):
        if message.get("role") == "user":
            last_user = message.get("content") or ""
            break

    if "记得" in last_user:
        return "tool", None

    return "text", None


class LLMHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        payload = json.loads(body.decode("utf-8", "replace"))
        log_line("llm.log", json.dumps(
            {"messages": [m.get("role") for m in payload.get("messages", [])],
             "tools": len(payload.get("tools") or [])}, ensure_ascii=False))

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()

        kind, text = decide(payload)
        if kind == "tool":
            self._stream_tool_call()
        else:
            chunks = REPLY_CHUNKS if text is None else [text]
            for chunk in chunks:
                self._send({"choices": [{"delta": {"content": chunk}}]})
                time.sleep(0.05)
            self._send({"choices": [{"delta": {}, "finish_reason": "stop"}]})

        self._finish()

    def _stream_tool_call(self):
        # 第一片：调用 ID 与函数名
        self._send({"choices": [{"delta": {"tool_calls": [{
            "index": 0, "id": "call_smoke_1", "type": "function",
            "function": {"name": TOOL_NAME, "arguments": ""},
        }]}}]})
        time.sleep(0.05)

        # 后续分片：参数逐片累积
        for piece in TOOL_QUERY_CHUNKS:
            self._send({"choices": [{"delta": {"tool_calls": [{
                "index": 0, "function": {"arguments": piece},
            }]}}]})
            time.sleep(0.05)

        self._send({"choices": [{"delta": {}, "finish_reason": "tool_calls"}]})

    def _send(self, payload):
        data = ("data: " + json.dumps(payload, ensure_ascii=False) + "\n\n").encode("utf-8")
        self.wfile.write(("%x\r\n" % len(data)).encode() + data + b"\r\n")
        self.wfile.flush()

    def _finish(self):
        done = b"data: [DONE]\n\n"
        self.wfile.write(("%x\r\n" % len(done)).encode() + done + b"\r\n")
        self.wfile.write(b"0\r\n\r\n")
        self.wfile.flush()

    def log_message(self, *args):
        pass


class TTSHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        log_line("tts.log", body.decode("utf-8", "replace"))

        frames = int(TTS_SAMPLE_RATE * TTS_SECONDS)
        pcm = b"".join(
            struct.pack("<h", int(3500 * math.sin(2 * math.pi * 520 * i / TTS_SAMPLE_RATE)))
            for i in range(frames)
        )

        self.send_response(200)
        self.send_header("Content-Type", "audio/pcm")
        self.send_header("Content-Length", str(len(pcm)))
        self.end_headers()
        self.wfile.write(pcm)

    def log_message(self, *args):
        pass


def serve(port, handler):
    ThreadingHTTPServer(("127.0.0.1", port), handler).serve_forever()


def main():
    os.makedirs(WORK_DIR, exist_ok=True)
    threading.Thread(target=serve, args=(LLM_PORT, LLMHandler), daemon=True).start()
    threading.Thread(target=serve, args=(TTS_PORT, TTSHandler), daemon=True).start()
    print("fake services ready", flush=True)
    while True:
        time.sleep(3600)


if __name__ == "__main__":
    sys.exit(main())
