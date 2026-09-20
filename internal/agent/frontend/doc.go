// Package frontend 提供前端接入（执行计划 E6）：/client-ws 下行协议、模型清单接口，
// 以及把播报队列合成好的音频送到浏览器播放。
//
// 本包是 broadcast.Sink 的实现：队列把 Item 与裸 PCM 交给 Play，这里负责
// 「打包成浏览器能直接播放的 WAV → 推送 → 等前端回执」，因此播报时序与浏览器
// 实际播放进度是对齐的——前一句放完，队列才继续下一句。
//
// 前端页面是自研的极简 HTML + 原生 JS（不打包、无构建步骤），渲染用
// pixi.js + pixi-live2d-display + Live2D Cubism Core，资产见 web/ 目录。
//
// 协议（自有定义，不追求与旧 Python 前端二进制兼容）：
//
//	服务端 → 浏览器
//	  {"type":"hello","data":{"character":{...},"model":{...}}}
//	  {"type":"speak","data":{"seq":1,"text":"...","emotion":3,"audio":"<base64 WAV>",...}}
//	浏览器 → 服务端
//	  {"type":"playback-started","data":{"seq":1}}
//	  {"type":"playback-finished","data":{"seq":1}}
//
// 音频统一是裸 PCM16 24kHz 单声道（tts 包的契约），这里补 44 字节 WAV 头再 base64，
// 浏览器用 <audio> 直接播，前端不需要做任何解码。
package frontend
