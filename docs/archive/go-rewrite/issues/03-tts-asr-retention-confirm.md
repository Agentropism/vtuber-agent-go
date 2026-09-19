Type: grilling
Status: resolved
Blocked by: 02 (已解决,本票现为 frontier,由地图维护者认领)

## Question

基于 02 引擎盘点表的最终保留清单确认:默认规则「远程 API 类全保留(edge-tts / openai-compatible / siliconflow / fish / minimax / gpt_sovits / x_tts),本地推理类全删除(bark / coqui / melo / sherpa_onnx / pyttsx3 / cosyvoice / spark 按封装成本另议)」;ASR 默认保留 groq / azure 远程 + 砍全部本地。

**重点决策:当前启用的 sherpa_onnx 双链替代路线**——
- A. sherpa-onnx 官方 Go binding(CGo,可加载同一 models/ 权重,零模型换血,但引入 CGo 构建链)
- B. sherpa-onnx 本地 HTTP 服务化 + Go HTTP 客户端(进程 +1,但 Go 侧零 CGo)
- C. 切云 API(TTS/ASR 全部云端,最简但离线无兜底、中文 ASR(SenseVoice)能力不可用)
- D. 组合:云为主链路 + 本地 sherpa HTTP 为兜底

另确认:音色克隆(coqui/spark/cosyvoice/gpt_sovits)是否需要,fun_asr 是否服务化保留作中文 ASR 增强。

## Resolution

grilling 用户拍板(2026-09-06,G1–G4):
1. **保留清单**:云 TTS 5 全保留(edge_tts / openai_tts / siliconflow_tts / fish_api_tts / minimax_tts);云 ASR 2 全保留(groq_whisper_asr / azure_asr)。**未选**:本地 HTTP 类(gpt_sovits / x_tts)、Gradio 类(cosyvoice / cosyvoice2 / spark)——全部删除。
2. **sherpa 双链替代路线:G2=A(Go binding)**——k2-fsa/sherpa-onnx 官方 Go binding(CGo),直接加载 models/ 现有 VITS + SenseVoice 权重,零模型换血;代价是 CGo 构建链。
3. **音色克隆:不做**(第一阶段角色声音用所选引擎 preset voice)。
4. **fun_asr:砍掉**(SenseVoice 经 Go binding 已覆盖中文 ASR)。

引擎级 failover 链(Go 设计):主引擎失败时按配置序降级,不再静默;音频输出统一 WAV PCM16(sherpa/云 API 各自转码),为 07 协议与 06 播报队列供统一格式。

## 进度：100%(2026-09-16 决议更新后关闭)

## 决议更新(2026-09-16)—— 上面第 1/2/4 条被推翻

盘点后用户重新拍板:

- **ASR 整体不做**。直播场景没有麦克风输入源(观众走弹幕、主播不说话),原保留的云 ASR 2 家(groq_whisper_asr / azure_asr)没有输入侧,属从 Open-LLM-VTuber 继承的遗留假设。`asr/` 包取消。
- **sherpa 双链不做**。原第 2 条 G2=A 的 CGo binding 路线作废:ASR 已不做,只剩 TTS 一条链,而云 TTS 已能覆盖;引入 CGo 会破坏「单二进制纯 Go」的构建与部署。计划风险表里的 CGo 风险随之消除。
- **TTS 仅云引擎**:保留清单收敛为 edge_tts / openai_tts / siliconflow_tts / fish_api_tts / minimax_tts 五家。
- **音频契约修订**(与上面第 5 段不同):统一为**裸 PCM16 24kHz 单声道**,不带 WAV 容器头;格式由 `tts` 包常量约定,需要落文件时由调用方自行补头。

落地情况:`tts/` 已实现(5 引擎 + failover + mp3 解码下混单声道 + 采样率兜底),
21 个用例覆盖请求形状、HTTP/业务错误码、hex 解码、重采样与构造校验。

下一步：无。原先指向 ticket 11 的「sherpa binding 引入方式」不再需要。