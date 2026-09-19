Type: research
Status: resolved

## Question

极简 HTML+Live2D 自研前端的可行渲染方案(研究):在浏览器中加载 `live2d-models/` 现有资产的最小方案对比——pixi-live2d-display / Live2D Cubism Web SDK / 其他;现有资产格式(Cubism 2/3,如 mao_pro、mini-maid)兼容性;音频驱动口型同步的最小实现;音频播放(Web Audio / audio 元素)与字幕、表情指令的协议建议;WebSocket 直连方案。

交付:方案对比表 + 推荐方案 + 关键 API/示例锚点,产出到 .scratch/go-rewrite/artifacts/01-frontend-live2d-stack.md。

## Resolution

已由 research subagent 完成(2026-09-06)。报告:`.scratch/go-rewrite/artifacts/01-frontend-live2d-stack.md`(中文,含资产盘点/方案对比/API 锚点/协议建议)。

核心结论:
1. 资产全部 Cubism 3(无 Cubism 2):mao_pro(8 表情 exp_01~08,LipSync ParamA + 五元音,有 HitArea)、shizuku(无表情,PARAM_MOUTH_OPEN_Y)。emo_map 在 model_dict.json(仅 mao_pro,值为 Expressions 下标);Go 端需复刻 extract_emotion([标签] 解析)+ remove_emotion_keywords(剥标签)。
2. 推荐方案:`@jannchie/pixi-live2d-display` 1.4.x(MIT,活跃)+ pixi.js 8 + 本地 live2dcubismcore.min.js(frontend/libs/ 已有,直接复用;官方直链不稳定不用)。原版 guansss 停更;官方 Web SDK 发布需签约,不推荐;live2d-widget 是 GPL 挂件,不用。
3. 杀手锏:fork 内置 model.speak(base64Wav, {volume, expression, resetExpression})——解码→播放→AnalyserNode 音量→自动驱动 LipSync 组 + 表情应用/复位,一条调用覆盖口型+表情。已知坑:speak() 停上一段须前端 FIFO 队列;iOS 需手势内 AudioContext.resume();shizuku 无 ExpressionManager 需判空;decodeAudioData detach 后 buffer 不可复用。
4. /client-ws 协议建议 5 条(供 07 采用):JSON 信封 {type,seq,ts,payload};type:"audio" 原子绑定 {audio base64 wav, expression, reset_expression, display_text}(一段 = 一次 speak);Go TTS 统一出 WAV PCM16 24kHz mono;上行回执 audio_started/audio_finished/error,流式靠 finished 推下一句;模型清单/文件走 HTTP(保留 /live2d-models/info 语义),WS 只传指令+音频;表情默认播完复位 neutral。
5. 沿用 ws://127.0.0.1:12393/client-ws 路径(端口并入 09 配置票)。

-> 07 已解锁。

## 进度：100%

下一步：07 grilling 采用本报告协议建议定稿。