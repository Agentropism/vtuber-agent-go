# Research Ticket 02 — TTS/ASR 引擎事实盘点(供 Go 重写决策)

> 研究范围:`Open-LLM-VTuber/src/open_llm_vtuber/tts/`(16 引擎)、`asr/`(7 引擎)、`config_manager/{tts,asr}.py` 配置模型、`config_templates/` 默认模板、`conf.yaml` 启用现状、`pyproject.toml` / `dockerfile` 依赖来源。
> 结论先行:**当前仅启用 sherpa_onnx_tts(本地推理)与 sherpa_onnx_asr(本地推理)——Go 重写若按"仅保留远程 HTTP API"原则,当前实际启用引擎全部落入删除候选,必须给出替代方案(见第三节缺口)。**
> 本报告不含任何密钥 / 凭据 / 敏感配置值。

---

## 0. 全局管线事实(先于引擎本身的结论)

- **多句分段不在引擎层**:LLM 输出先经 `utils/sentence_divider.py`(SentenceDivider)+ `agent/transformers.py` 的 `sentence_divider` 装饰器按标点切句(pysbd/regex 两种分段器,可配 `faster_first_response` 首句逗号提前),逐句进入 TTS。
- **并行合成在管线层**:`conversations/tts_manager.py` 的 `TTSTaskManager` 为每个句子创建独立 asyncio task **并行**调 `tts_engine.async_generate_audio()`,按序号经队列**有序**投递 WS 给前端。引擎自身只需"单句 → 音频文件"能力。
- **引擎输入输出契约**:文本 → `cache/<时间戳>_<uuid8>.<ext>` 音频文件路径;失败返回 `None`/`""`,上层降级为"静默 payload"(不出声但照常发显示文本)。
- **流式语义**:项目没有"实时边合成边播放"音频流;所谓流式仅是 HTTP/SSE 层面的分块下载。引擎的单次调用全是"一次性合成整段音频文件"。
- **sherpa_onnx_tts 例外**:引擎内部有 `max_num_sentences`(默认 2)批内多句合并(单次调用文本可含多句送 VITS 批处理)。

---

## 1. 引擎盘点表

### 1.1 TTS(16 个)

| 引擎(文件名 / 引擎名) | 协议类型 | 依赖库 | 本地模型权重 | 配置键(YAML 块 / 键) | 流式 / 多句 | 当前启用 |
|---|---|---|---|---|---|---|
| edge_tts | 远程 HTTP API(微软 Edge 免费合成,经 WebSocket→bing speech 服务) | `edge-tts`(pyproject 主依赖) | 无 | `tts_model: edge_tts`;块 `edge_tts: {voice}` | 底层可流式但实现用 `save_sync` 一次性保存 mp3;单句 | 否(conf.yaml 有块) |
| azure_tts | 远程 HTTP API(Azure Speech 云,SDK 封装 WS/REST) | `azure-cognitiveservices-speech`(主依赖) | 无 | `tts_model: azure_tts`;块 `azure_tts: {api_key, region, voice, pitch, rate}` | 非流式;SSML 包裹一次合成 wav;单句 | 否(有块) |
| openai_tts | 远程 HTTP API(OpenAI 兼容 `/v1/audio/speech`,可指向本地 kokoro 服务器) | `openai` SDK(主依赖) | 无(服务器侧可能本地推理) | `tts_model: openai_tts`;块 `openai_tts: {model, voice, api_key, base_url, file_extension}` | `with_streaming_response` 流式写文件;单句 | 否(conf.yaml **无**此块) |
| cosyvoice_tts | 远程 HTTP API(本机 CosyVoice Gradio WebUI,gradio 协议) | `gradio_client`(选装,非 pyproject 主依赖) | 无(WebUI 侧推理) | `tts_model: cosyvoice_tts`;块 `cosyvoice_tts: {client_url, mode_checkbox_group, sft_dropdown, prompt_text, prompt_wav_upload_url, prompt_wav_record_url, instruct_text, seed, api_name}` | 非流式 predict;单句 | 否(无块) |
| cosyvoice2_tts | 远程 HTTP API(同上,`/generate_audio`) | `gradio_client`(选装) | 无 | `tts_model: cosyvoice2_tts`;块 `cosyvoice2_tts: {client_url, mode_checkbox_group, sft_dropdown, prompt_text, prompt_wav_upload_url, prompt_wav_record_url, instruct_text, stream, seed, speed, api_name}` | 有 stream 参数但 predict 路径未用实时流;单句 | 否(无块) |
| bark_tts | 本地模型推理(需下载权重,bark `preload_models` 自动下载到缓存) | `bark`(git 安装,选装)、torch、scipy | **是**(自动下载;macOS 有 MPS 坑) | `tts_model: bark_tts`;块 `bark_tts: {voice}` | 非流式 wav;单句 | 否(有块) |
| coqui_tts | 本地模型推理(TTS 库,首次运行自动下载 HF 模型) | `TTS`(coqui,选装)、torch | **是**(自动下载) | `tts_model: coqui_tts`;块 `coqui_tts: {model_name, speaker_wav, language, device}` | 非流式 wav;单句;支持 speaker_wav 声音克隆 | 否(有块) |
| gpt_sovits_tts | 远程 HTTP API(本机 GPT-SoVITS 服务 `:9880/tts`,GET + query) | `requests` | 无(服务侧推理) | `tts_model: gpt_sovits_tts`;块键名 `gpt_sovits_tts: {api_url, text_lang, ref_audio_path, prompt_lang, prompt_text, text_split_method, batch_size, media_type, streaming_mode}`(pydantic 字段 alias 为 `gpt_sovits`,靠 `populate_by_name=True` 兜底匹配) | 一次 GET 响应;`streaming_mode` 参数存在但未真正流式消费;单句 | 否(有块) |
| fish_api_tts | 远程 HTTP API(Fish Audio 云 `api.fish.audio`) | `fish_audio_sdk`(选装,非主依赖) | 无 | `tts_model: fish_api_tts`;块 `fish_api_tts: {api_key, reference_id, latency, base_url}` | SDK `Session.tts` 返回 chunk 迭代器,**分块(流式)写文件**;单句 | 否(有块) |
| melo_tts | 本地模型推理(MeloTTS,权重放 `models/`) | `melo.api`(dockerfile `melo/init_downloads.py` 预下载)、torch | **是**(`models/vits-melo-tts-zh_en` 已存在) | `tts_model: melo_tts`;块 `melo_tts: {speaker, language, device, speed}` | 非流式 wav;单句 | 否(有块) |
| minimax_tts | 远程 HTTP API(Minimax 云 `api.minimax.chat` `/v1/t2a_v2`) | `requests` | 无 | `tts_model: minimax_tts`;块 `minimax_tts: {group_id, api_key, model, voice_id, pronunciation_dict}` | **SSE `stream=true` 分块**,hex 音频 chunk 拼装成 mp3;单句 | 否(有块) |
| pyttsx3_tts | 本地系统调用(OS 级 TTS:SAPI / NSSpeechSynthesizer / espeak 等) | `pyttsx3`(主依赖) | 无(用系统语音) | 工厂注册值 `pyttsx3_tts`,但 **TTSConfig Literal 未收录**(配置模型缺失;conf.yaml 无块) | 非流式 aiff;单句;带线程锁(非线程安全) | 否(配置模型不支持) |
| sherpa_onnx_tts | 本地模型推理(sherpa-onnx VITS ONNX,权重在 `models/`) | `sherpa-onnx`、`soundfile`、onnxruntime(均主依赖) | **是**(`vits_model` 指向模型文件,模板示例 `models/vits-melo-tts-zh_en`) | `tts_model: sherpa_onnx_tts`;块 `sherpa_onnx_tts: {vits_model, vits_lexicon, vits_tokens, vits_data_dir, vits_dict_dir, tts_rule_fsts, max_num_sentences, sid, provider, num_threads, speed, debug}` | 非流式 wav;**引擎内 `max_num_sentences`(默认 2)批内多句合并** | **是(当前启用)** |
| siliconflow_tts | 远程 HTTP API(SiliconFlow 云 `/v1/audio/speech`,OpenAI 兼容) | `requests` | 无 | `tts_model: siliconflow_tts`;块 `siliconflow_tts: {api_url, api_key, default_model, default_voice, sample_rate, response_format, stream, speed, gain}` | `stream` 参数传给 API,但实现一次性 `response.content` 存文件;单句 | 否(有块) |
| spark_tts | 远程 HTTP API(本机 SparkTTS Gradio WebUI `:7860`,`/voice_clone` 或 `/voice_creation`) | `gradio_client`(选装) | 无(WebUI 侧推理) | `tts_model: spark_tts`;块 `spark_tts: {api_url, prompt_wav_upload, api_name, gender, pitch, speed}` | 非流式;`api_name` 支持 voice_clone / voice_creation 两种;单句 | 否(有块) |
| x_tts | 远程 HTTP API(本机 XTTS 服务器 `:8020/tts_to_audio`,POST JSON;服务侧通常本地推理) | `requests` | 无(服务侧) | `tts_model: x_tts`;块 `x_tts: {api_url, speaker_wav, language}` | 一次 POST 响应;单句 | 否(有块) |

### 1.2 ASR(7 个)

| 引擎(文件名 / 引擎名) | 协议类型 | 依赖库 | 本地模型权重 | 配置键(YAML 块 / 键) | 流式 / 多句 | 当前启用 |
|---|---|---|---|---|---|---|
| openai_whisper_asr(`whisper`) | 本地模型推理(openai-whisper,首次运行下载权重) | `openai-whisper`(dockerfile 选装)、torch | **是**(自动下载) | `asr_model: whisper`;块 `whisper: {name, download_root, device}` | 一次性整段转写;支持 prompt 上下文 | 否(有块) |
| faster_whisper_asr(`faster_whisper`) | 本地模型推理(faster-whisper CTranslate2,自动下载) | `faster_whisper`(选装,非 pyproject 主依赖) | **是**(自动下载) | `asr_model: faster_whisper`;块 `faster_whisper: {model_path, download_root, language, device, compute_type}` | 一次性整段转写(beam_size=5 可关);`condition_on_previous_text=False` | 否(有块) |
| fun_asr(`fun_asr`) | 本地模型推理(FunASR/ModelScope,首次运行自动下载;VAD+标点组合) | `funasr`、`modelscope`、`huggingface_hub`(dockerfile 选装)、torch | **是**(ModelScope 缓存自动下载) | `asr_model: fun_asr`;块 `fun_asr: {model_name, vad_model, punc_model, device, disable_update, ncpu, hub, use_itn, language}` | 一次性整段转写;输出剥 SenseVoice 标签;支持 VAD/标点/说话人模型组合 | 否(有块) |
| groq_whisper_asr(`groq_whisper_asr`) | 远程 HTTP API(Groq Cloud,multipart 上传 wav 转写) | `groq` SDK(pyproject 主依赖) | 无 | `asr_model: groq_whisper_asr`;块 `groq_whisper_asr: {api_key, model, lang}` | 一次性整段转写(内存 wav → multipart);`temperature=0` | 否(有块) |
| azure_asr(`azure_asr`) | 远程 HTTP API(Azure Speech 云识别) | `azure-cognitiveservices-speech`(主依赖) | 无 | `asr_model: azure_asr`;块 `azure_asr: {api_key, region, languages}` | `recognize_once` 一次性;支持多语言自动检测(默认 en-US, zh-CN);临时 wav 文件上传 | 否(有块) |
| sherpa_onnx_asr(`sherpa_onnx_asr`) | 本地模型推理(sherpa-onnx;sense_voice 模型缺失时**自动从 GitHub Release 下载**至 `models/`) | `sherpa-onnx`、`onnxruntime`(主依赖) | **是**(`models/sherpa-onnx-sense-voice-...` 已存在;另有 transducer/paraformer/nemo_ctc/wenet_ctc/whisper/tdnn_ctc 多模式) | `asr_model: sherpa_onnx_asr`;块 `sherpa_onnx_asr: {model_type, encoder, decoder, joiner, paraformer, nemo_ctc, wenet_ctc, tdnn_model, whisper_encoder, whisper_decoder, sense_voice, tokens, num_threads, use_itn, provider}`(model_type 为必填,按类型校验对应路径) | 一次性整段转写;SenseVoice 支持 ITN | **是(当前启用)** |
| whisper_cpp_asr(`whisper_cpp`) | 本地模型推理(pywhispercpp/GGML,权重在 `asr/models` 目录) | `pywhispercpp`(dockerfile 选装) | **是**(`model_dir` 指定,下载) | `asr_model: whisper_cpp`;块 `whisper_cpp: {model_name, model_dir, print_realtime, print_progress, language}` | 一次性整段转写;`new_segment_callback` 仅日志 | 否(有块) |

### 1.3 当前 conf.yaml 启用现状(仅键名与引擎名,无任何值/凭据)

- `character_config → asr_config → asr_model: 'sherpa_onnx_asr'`(块内键:model_type、sense_voice、tokens、num_threads、use_itn、provider)
- `character_config → tts_config → tts_model: 'sherpa_onnx_tts'`(块内键:vits_model、vits_lexicon、vits_tokens、vits_data_dir、vits_dict_dir、tts_rule_fsts、max_num_sentences、sid、provider、num_threads、speed、debug)
- conf.yaml 中**已有配置块**(仅键名存在,不代表启用):TTS 12 块 = azure_tts、bark_tts、edge_tts、melo_tts、x_tts、gpt_sovits_tts、fish_api_tts、coqui_tts、sherpa_onnx_tts、siliconflow_tts、spark_tts、minimax_tts(缺 cosyvoice_tts / cosyvoice2_tts / openai_tts / pyttsx3_tts 块);ASR 7 块全 = azure_asr、faster_whisper、whisper_cpp、whisper、fun_asr、sherpa_onnx_asr、groq_whisper_asr。
- 默认模板 `config_templates/conf.default.yaml`:`tts_model: 'edge_tts'`、`asr_model: 'sherpa_onnx_asr'`。
- `models/` 目录现有本地权重:`sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17`(目录+tar 归档)与 `vits-melo-tts-zh_en`(目录+tar 归档),对应 sherpa_onnx_asr(sense_voice)与 sherpa_onnx_tts/melo_tts。

---

## 2. 远程 HTTP API 类 → **保留候选**(Go 重写需封装)

> 全部为"文本/音频 → HTTP(S) 请求 → 音频/文本文件",无本地权重、无 torch,Go 标准库 net/http 即可实现,唯一技术债是 cosyvoice/spark 的 **Gradio 协议**(需按 gradio client v4 协议实现 `/call` 提交 + 轮询进度 + 结果下载,或改走 WebUI 原始 API)。

### 2.1 云服务(公网 API,需鉴权外发)

| 引擎 | 端点形态 | HTTP 调用要点 |
|---|---|---|
| edge_tts | 微软 Edge 免费服务(WebSocket `speech.platform.bing.com`,社区逆向协议) | 无 API key;需伪造 `Sec-MS-GEC`/token 与 WSS 握手;地区封锁风险(Python 注释明示"blocked in your region");Go 侧可选现成库(`gosimple/edge-tts` 类)或封装 WSS;输出 mp3 流 |
| azure_tts | Azure Speech 云(REST `/cognitiveservices/v1` 或 WS) | 鉴权:`Ocp-Apim-Subscription-Key` 请求头(或 OAuth token);URL 带 `region`;SSML 正文控制 pitch/rate/voice;输出 wav(Riff16Khz16BitMonoPCM) |
| openai_tts | `POST {base_url}/v1/audio/speech`(OpenAI 兼容,可指本地) | 鉴权:`Authorization: Bearer <key>`(本地服务器可为空/任意);JSON body `{model, voice, input, response_format, speed}`;响应为二进制音频流,可边下边写;默认端点 `http://localhost:8880/v1`(kokoro) |
| fish_api_tts | `POST https://api.fish.audio/v1/tts`(SDK 封装) | 鉴权:SDK 用 apikey(@fish-audio token,请求头 `Authorization: Bearer <key>`);body `{text, reference_id, latency, format}`;响应为**音频 chunk 流**,逐块写文件 |
| minimax_tts | `POST https://api.minimax.chat/v1/t2a_v2?GroupId=<group_id>` | 鉴权:`Authorization: Bearer <key>` + **GroupId 作为 query 参数**;body 含 `stream: true` → SSE,逐行 `data:` JSON 中 `data.audio` 为 hex 音频拼装;输出 mp3 |
| siliconflow_tts | `POST https://api.siliconflow.cn/v1/audio/speech`(OpenAI 兼容) | 鉴权:`Authorization: Bearer <key>`;body `{model, voice, input, response_format, sample_rate, stream, speed, gain}`;默认模型 `FunAudioLLM/CosyVoice2-0.5B`;一次性响应写文件(stream 参数虽可传但当前实现整体接收) |
| groq_whisper_asr | `POST https://api.groq.com/openai/v1/audio/transcriptions` | 鉴权:`Authorization: Bearer <key>`;**multipart/form-data**:`file`(wav 16k/1ch/16bit 内存构造)+ `model` + `language` + `temperature=0` + `response_format=text`;返回纯文本 |
| azure_asr | Azure Speech 云识别 | 鉴权:`Ocp-Apim-Subscription-Key` + region;本地先写临时 wav(16k PCM_16)再上传/发送;`recognize_once` 单次识别;支持多语言自动检测(languages 列表) |

### 2.2 本地/自建 HTTP 服务(旁路推理,Go 侧仍为普通 HTTP 客户端)

| 引擎 | 端点形态 | HTTP 调用要点 |
|---|---|---|
| gpt_sovits_tts | `GET http://127.0.0.1:9880/tts` | **无鉴权**(本地);query 参数:text、text_lang、ref_audio_path、prompt_lang、prompt_text、text_split_method、batch_size、media_type、streaming_mode;Python 侧先剥 `[...]` 标注文本;120s 超时;响应体即音频(media_type 可选 wav) |
| x_tts | `POST http://127.0.0.1:8020/tts_to_audio` | 无鉴权(本地);JSON `{text, speaker_wav, language}`;120s 超时;响应体即 wav |
| cosyvoice_tts / cosyvoice2_tts | `http://127.0.0.1:50000/` Gradio `/generate_audio` | 无鉴权;Gradio 协议(提交参数含 mode/sft/prompt_wav 参考音/seed 等,返回生成音频路径);cosyvoice2 多 stream、speed 参数 |
| spark_tts | `http://127.0.0.1:7860/` Gradio `/voice_clone` 或 `/voice_creation` | 无鉴权;voice_clone 需参考音频文件(prompt_wav_upload);voice_creation 用 gender/pitch/speed;结果文件需复制到 cache 目录 |

---

## 3. 本地推理类 → **删除候选**(含 Go 侧能力缺口)

| 引擎 | 现状 | Go 侧能力缺口 / 替代思路 |
|---|---|---|
| sherpa_onnx_tts | **当前启用**(VITS,`models/vits-melo-tts-zh_en`) | 缺口:本地离线 TTS 通道消失,直播主播场景将完全依赖外部 API。替代:① k2-fsa/sherpa-onnx 官方提供 **Go bindings**(CGo,可加载同一 VITS ONNX 模型,零缺口保留);② 或把 sherpa-onnx 起为 HTTP 服务(sherpa-onnx 有 offline TTS server 示例)由 Go 调;③ 或切 edge_tts/openai_tts(kokoro 本地) |
| sherpa_onnx_asr | **当前启用**(SenseVoice,`models/sherpa-onnx-sense-voice-...`) | 缺口:中文 ASR 主通道(低延迟、离线、直播场景核心)。替代:① 同上 sherpa-onnx Go bindings 直接复刻;② whisper.cpp Go 绑定(go-whisper)加载 GGML;③ 切 groq/azure 云 ASR(需网络与费用) |
| bark_tts | 未启用(本地,需下载权重) | 缺口:无(非生产依赖);纯删除即可;若有英文语音需求 edge_tts 覆盖 |
| coqui_tts | 未启用(本地,自动下载) | 缺口:声音克隆能力(coqui xtts 风格 speaker_wav);若产品需要克隆音色,Go 侧无官方等价,建议走 openai_tts(kokoro)或 GPT-SoVITS 本地服务 |
| melo_tts | 未启用(本地,权重已在 models/ 且与 sherpa VITS 同源) | 缺口:无独立缺口(sherpa_onnx_tts 可加载同类 MeloTTS VITS 模型);删除后 models/ 中 vits-melo-tts-zh_en 可移交 sherpa 通道 |
| pyttsx3_tts | 未启用且**配置模型缺失**(TTSConfig Literal 无此值) | 缺口:系统级离线 TTS(Windows SAPI/macOS 语音/linux espeak)。Go 侧替代:Windows 调 PowerShell `System.Speech`(COM),macOS `osascript` 调 NSSpeechSynthesizer,Linux exec `espeak-ng`/`pico2wave`;属平台特定薄壳,可做可不做 |
| openai_whisper_asr | 未启用(本地,自动下载权重) | 缺口:无(whisper.cpp Go 绑定或云 API 覆盖);删除 |
| faster_whisper_asr | 未启用(本地,自动下载) | 缺口:无(同上);删除;注意其默认 int8 CPU 效果好,若将来要离线中文 ASR,whisper.cpp 是 Go 侧最近等价物 |
| fun_asr | 未启用(本地,自动下载) | 缺口:**VAD+标点+说话人分离**(SenseVoice/paraformer 组合是管线里最强的中文能力);若产品依赖它,建议保留为独立 HTTP 服务(FunASR 官方有 server),Go 侧仅做客户端 |
| whisper_cpp_asr | 未启用(本地,`asr/models` 权重) | 缺口:无;Go 侧 go-whisper 直接等价;删除(如需离线下可选它做替代) |

> 删除后的总缺口归纳:① 当前启用的两条引擎链(sherpa_onnx_tts + sherpa_onnx_asr)都需替代——首选 **sherpa-onnx 官方 Go binding**(同一模型零损失),次选本地服务化 + Go 客户端,再选云 API 切换;② 声音克隆/音色定制能力(coqui/spark/cosyvoice/gpt_sovits 侧)依赖外部服务,Go 重写后仍需在"本地服务"或"云 API"中保留至少一条音色可定制通道;③ AFK/离线场景(直播断网)本地 TTS/ASR 全删后没有兜底。

---

## 4. Go 重写封装每引擎的注意点

### 4.1 统一契约(先于各引擎)

- **接口**:`Synthesize(ctx, text) (audio []byte, format string, err error)` / `Transcribe(ctx, wav16kPCM) (text string, err error)`;超时/取消一律走 `context.Context`。
- **文件与生命周期**:Python 侧是"写 cache 文件 → WS 发路径 → 前端下载 → remove_file";Go 侧建议改为**内存返回字节 + 流式 WS 二进制帧**,省掉文件生命周期与并发清理(tts_manager 现为并行任务 + 有序投递,Go 用 goroutine + 序号缓冲队列等价实现)。
- **多句分段**:沿用管线层切句;每句一个 goroutine 并行合成,结果按序号有序发送;首句可继续做"逗号提前"加速(agent 配置 `faster_first_response`)。
- **格式归一**:各引擎输出 mp3/wav/aiff 不一,前端与打断逻辑需要统一音频格式(建议统一转 16k/24k mono PCM 或 mp3;Go 侧 `goav`/`ffmpeg` exec 或纯 Go wav 处理;pyttsx3 的 aiff 尤其特殊)。

### 4.2 各引擎注意点(鉴权头 / 错误降级 / 超时)

| 引擎 | 鉴权 | 超时建议 | 错误降级要点 |
|---|---|---|---|
| edge_tts | 无需用户 key(伪造 WS token) | 连接+整体 20~30s | 地区封锁会报错(Python 注释明示);降级链主选;失败返回错误让上层切备用 |
| azure_tts | `Ocp-Apim-Subscription-Key` / OAuth | 30~60s | region 配置错/欠费返回 `Canceled`+error_details;SSML 转义文本防注入;pitch/rate 必填校验(pydantic 强制) |
| openai_tts | `Bearer <key>`;本地服务可忽略 | 30s(长句可放宽) | base_url 可指向任意 OpenAI 兼容服务(含 kokoro 本地);`file_extension` 仅 mp3/wav 白名单;非 2xx 时清理半截文件 |
| cosyvoice/cosyvoice2 | 无 | Gradio 轮询需长超时(合成可能 30s+) | Gradio 协议需实现提交+轮询+下载三步;client_url 默认 `:50000`;prompt_wav 默认引用远端示例音频,可改成空/本地参考音 |
| gpt_sovits_tts | 无(本地) | 120s(Python 已设) | GET 语义(注意不是 POST);先剥 `[...]` 标注;ref_audio_path 缺失会失败,需配置校验 |
| fish_api_tts | `Bearer <key>` | 30~60s | SDK 是分块迭代,Go 直接读响应体流;latency 枚举 normal/balanced;reference_id 无效返回 4xx,记日志不崩溃 |
| melo/bark/coqui/sherpa(onxx) | 不适用(CGo 或删除) | 推理本身阻塞 → CGo 放 worker 池 | 模型路径校验先行(sherpa `validate()`);provider 降级(cuda→cpu 有现成逻辑);显存不足捕获 |
| minimax_tts | `Bearer <key>` + GroupId query | SSE 读超时 30s+ | SSE 逐行解析容错(跳过非 `data:` 行与 `extra_info` 包);hex 解码失败跳过该 chunk 不中断;stream 固定 true |
| siliconflow_tts | `Bearer <key>` | 30~60s | api_url 未配置时 Python 返回空串**不报异常**(坑);Go 侧应做启动期必填校验;stream 参数建议按实际响应处理 |
| pyttsx3_tts | 不适用 | 系统调用可能挂起 → exec 超时+kill | 非线程安全(Python 加锁);Go 侧全局串行化或直接弃用 |
| spark_tts | 无(本地) | Gradio 轮询 30~60s | `api_name` 分支 voice_clone/voice_creation;参考音频文件路径校验;结果需复制到 cache |
| x_tts | 无(本地) | 120s(Python 已设) | speaker_wav 是字符串名而非路径(默认 "female"),服务端解释;语言参数传服务端 |
| groq_whisper_asr | `Bearer <key>` | 30s(上传 16k wav) | multipart 构造内存 wav(16k/1ch/16bit);model 默认 `whisper-large-v3-turbo`;超长音频受 API 时长限制,丢给降级链 |
| azure_asr | `Ocp-Apim-Subscription-Key` + region | 30s | NoMatch 返回空串(不是错误,语义要区分);Canceled 才抛错;临时 wav 文件用完即删 |
| faster_whisper / whisper / whisper_cpp | 不适用 | 推理阻塞 → worker 池 | 模型自动下载失败、设备不可用都要启动期暴露;beam_size=5 可关;`condition_on_previous_text=False` 已防重复 |
| fun_asr | 不适用 | 推理阻塞 → worker 池 | SenseVoice 输出带 `<|zh|>` 等标签需正则剥离(python 已实现,Go 复刻);modelscope 下载失败降级 HF(hub 参数) |
| sherpa_onnx_asr | 不适用 | 推理阻塞 | sense_voice 模型缺失时自动下载(github release,tar.bz2 解压到 models/);model_type 各模式路径校验(pydantic validator 对应 Go 侧 struct tag 必填组) |

### 4.3 其他工程注意点

- **配置兼容**:Go 侧配置模型需复刻 `TTSConfig.tts_model` Literal(15 值,**不含 pyttsx3_tts**——Python 侧自身配置模型就漏了这个factory值,Go 重写可顺手去掉或补上)与 `ASRConfig.asr_model` Literal(7 值);YAML 块名即引擎名;`gpt_sovits_tts` 块键名与 pydantic alias(`gpt_sovits`)不一致,靠 `populate_by_name` 兜底,Go 侧直接用块键名即可,无此问题。
- **降级链**:Python 无引擎级 failover(失败仅静默);Go 重写建议一次性做对:TTSType/ASRType 支持主备列表,失败自动切下一引擎并打日志。
- **并发模型**:t TTS 并行(每句 goroutine)+ ASR 串行单次;本地推理引擎(如保留 sherpa CGo)需限制并发(worker 池/信号量),云 API 需限流保护;minimax/edge 高频调用可能触发服务端风控。
- **依赖裁剪**:清除本地推理后,pyproject 可删 torch/scipy/sherpa-onnx/onnxruntime 等重依赖;**openai、groq、requests/HTTP 类与 edge-tts(WSS 实现)是保留候选的依赖主力**。

---

## 5. 附:一次结论摘要(用于决策层)

1. 当前部署实际只有 **sherpa_onnx_tts(本地 VITS)+ sherpa_onnx_asr(本地 SenseVoice)** 两条链在跑,都是本地推理 —— 按"只保留远程 HTTP API"前提,两者都进删除候选,**必须先在 Go 侧确定替代**(推荐 sherpa-onnx 官方 Go binding 无损失复刻,或本地服务化 + Go 客户端,或改云 API)。
2. 保留候选(无需选全部,建议按产品取舍):
   - ASR 云:`groq_whisper_asr`(免费额度、快)、`azure_asr`(稳定、多语言)。
   - TTS 云:`edge_tts`(零成本、多语言,但地区封锁风险)、`openai_tts`(OpenAI 兼容面最广,可接本地 kokoro)、`siliconflow_tts`/`fish_api_tts`/`minimax_tts`(国内可直连、音色池)。
   - TTS 本地服务(HTTP 封装即可拿下的实用通道):`gpt_sovits_tts`(强音色克隆)、`x_tts`、`cosyvoice2_tts`/`cosyvoice_tts` 与 `spark_tts`(Gradio 协议,封装成本最高)。
3. 删除候选:本地推理类全部(bark/coqui/melo/pyttsx3 + whisper 三件套/fun_asr/sherpa 两件套),pyttsx3 顺带(配置模型本就缺失)。
4. Go 重写最大的架构收益机会:放弃"文件落盘 + WS 传路径"范式,改为内存字节 + 二进制帧流式下发;放弃"每引擎各自 requests 裸调",统一 `httpx`-风格 client(超时、重试、Bearer 注入、SSE 解析)。

---

*报告产出:research ticket 02,2026-09-06。源码依据:`src/open_llm_vtuber/tts/*.py`、`src/open_llm_vtuber/asr/*.py`、`config_manager/{tts,asr,main,character,i18n}.py`、`conversations/tts_manager.py`、`utils/sentence_divider.py`、`agent/transformers.py`、`config_templates/conf.default.yaml`、`conf.yaml`(仅键名)、`pyproject.toml`、`dockerfile`、`models/` 目录列表。*