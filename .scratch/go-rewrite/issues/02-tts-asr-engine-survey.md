Type: research
Status: resolved

## Question

盘点 Open-LLM-VTuber 全部 TTS/ASR 引擎的接入类型与依赖(Fact 调研):逐个引擎标注——协议类型(远程 HTTP API / 本地推理 / 子进程)、所需依赖与模型下载、配置键(conf.yaml 中对应键名)、当前 conf.yaml 实际启用项。覆盖:tts/ 全部实现(edge/azure/openai/cosyvoice×2/bark/coqui/gpt_sovits/fish/melo/minimax/pyttsx3/sherpa/siliconflow/spark/x_tts)+ asr/(openai_whisper/faster_whisper/fun_asr/groq/sherpa/whisper_cpp/azure)。

产出:引擎清单表(协议类型/依赖/配置键/是否启用)+ 远程 API 类保留候选表 + 本地类删除候选表,写到 .scratch/go-rewrite/artifacts/02-tts-asr-engine-survey.md。

## Resolution

已由 research subagent 完成(2026-09-06)。报告:`.scratch/go-rewrite/artifacts/02-tts-asr-engine-survey.md`(16 TTS + 7 ASR 逐引擎定性,无凭据)。

核心结论:
1. conf.yaml 当前仅启用 `sherpa_onnx_tts` + `sherpa_onnx_asr`——**均为本地推理**,按「仅远程 API」前提双双入删除候选,Go 侧必须定替代路线(sherpa-onnx 官方 Go binding / 本地服务化 / 切云 API)。
2. 保留候选:云 TTS = edge / openai(可接本地 kokoro)/ siliconflow / fish / minimax;本地 HTTP 薄封装 = gpt_sovits / x_tts;云 ASR = groq / azure;cosyvoice / spark 需 Gradio 协议,封装成本最高。
3. 删除候选缺口:离线无兜底、音色克隆依赖外部服务、fun_asr 中文能力(含 VAD/说话人分离)建议服务化保留。
4. Go 架构事实:多句分段在 Agent 输出层(sentence_divider),并行合成在 TTSTaskManager,引擎=「单句→音频」;故障现为静默降级,Go 建议内存字节 + WS 二进制帧 + 引擎级 failover 链。
5. 小坑:gpt_sovits YAML 块键名与 pydantic alias 不一致(populate_by_name 兜底,Go 直接用块键名);minimax 鉴权 = Bearer + GroupId(或 group_id query);siliconflow 未配 api_url 返回空串不报错。

-> 03 已解锁,重点先拍板 sherpa 双链替代路线。

## 进度：100%

下一步：03 grilling 确认引擎保留清单与 sherpa 替代路线。