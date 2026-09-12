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

## 进度：100%

下一步：11 仓库骨架将按此清单落地引擎包边界;sherpa Go binding 的引入方式(子模块/gomod)在 11 中确认。