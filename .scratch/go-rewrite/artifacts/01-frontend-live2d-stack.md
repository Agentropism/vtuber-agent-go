# 01 · 前端 Live2D 渲染栈调研报告(Go 重写项目)

> 调研日期:2026-09-06 | 范围:浏览器端 Live2D 最小可行加载方案、口型同步、表情切换、Go 后端 `/client-ws` 协议建议
> 目标:前端从 React 自研改为「极简 HTML + Live2D」单页(原生 JS + WebSocket 直连 Go 后端),复用现有 `live2d-models/` 资产。
> 数据来源:本地资产与代码实测 + 官方文档/仓库/NPM Registry 核实(见文末参考链接)。未修改任何源代码。

---

## 1. 资产盘点结论

**全部为 Cubism 3 格式(`.moc3` + `.model3.json`),共 2 个模型**,不存在 Cubism 2(`.moc`/`.model.json`)资产:

| 模型 | 格式 | 纹理 | 表情 (Expressions) | 动作 (Motions) | LipSync 组 | 其他 |
|---|---|---|---|---|---|---|
| `mao_pro` | Cubism 3 (Version 3) | 1×4096 PNG | **8 个**:exp_01~exp_08 (`.exp3.json`) | Idle + 6 个 (`.motion3.json`) | **`ParamA`** | physics3/pose3/cdi3 齐全,HitAreas 有(头/身) |
| `shizuku` | Cubism 3 (Version 3) | 5×1024 PNG | **无**(model3.json 未声明 Expressions) | FlickUp/Tap/Flick3/Idle 各 1 | **`PARAM_MOUTH_OPEN_Y`** | cdi3 齐全,无 HitAreas |

实测依据:
- 两模型根配置 `Version: 3`,`FileReferences.Moc` 指向 `.moc3`(非 `.moc`);无任何 `.moc` / `.model.json` 文件。
- `mao_pro.cdi3.json` 参数含 5 元音口型 `ParamA/ParamI/ParamU/ParamE/ParamO`(可做精细口型),`model3.json` 的 `Groups[LipSync] = ["ParamA"]`。
- `shizuku.model3.json` 的 `Groups[LipSync] = ["PARAM_MOUTH_OPEN_Y"]`。

**emo_map(表情标签)现状**(`Open-LLM-VTuber/model_dict.json`,仅收录 mao_pro):

```json
{"emotionMap": {"neutral": 0, "anger": 2, "disgust": 2, "fear": 1, "joy": 3, "smirk": 3, "sadness": 1, "surprise": 3}}
```

- 语义:LLM 输出文本中的 `[标签]`(如 `[joy]`)→ Python `live2d_model.py::extract_emotion()` 按 `emo_map` 查表得到 **表达式数组下标**(0 基)→ `exp_01`=0 … `exp_04`=3(实际只用 0~3)。
- Go 重写需复刻同一逻辑:`extract_emotion`(解析 `[tag]`)与 `remove_emotion_keywords`(从字幕文本中剥掉标签)。

**现有前端基线**(`frontend/` 已构建产物,供协议兼容参考):
- 栈 = pixi-live2d-display + PixiJS + 双 Core(`libs/live2dcubismcore.min.js` 与 `libs/live2d.min.js` 都在,含 Cubism 2 核心,但当前无 Cubism 2 资产)。
- 连接地址即 **`ws://127.0.0.1:12393/client-ws`**(原 Python 端路由 `routes.py::init_client_ws_route`,Go 应沿用同名路径)。
- 下行载荷(见 `utils/stream_audio.py::prepare_audio_payload`):`{"type":"audio", "audio": <base64 wav>, "volumes": [RMS 归一化数组], "slice_length": 20ms, "display_text": ..., "actions": {"expressions": [索引数组]}}`。
- bundle 内口型实现:`setParameterValueById(this._lipSyncParameterIds.at(dt), ht)`(走 model3.json LipSync 组),表情 `setExpression(...)`。

---

## 2. 方案对比

| 方案 | 许可 | 维护状态 | 资产兼容 (Cubism3) | 口型支持 | 集成成本 |
|---|---|---|---|---|---|
| **pixi-live2d-display(guansss 原版)** [源码](https://github.com/guansss/pixi-live2d-display) [npm](https://www.npmjs.com/package/pixi-live2d-display) | MIT | **已停更**:latest 0.4.0 (2022-09-04, 配 pixi v6);0.5.0-beta (2023-12, 配 pixi v7);仓库未归档但无实质更新 | ✅(cubism4 bundle + `live2dcubismcore.min.js`)| 无内置口型 API,需自写 rAF+AnalyserNode 或喂 volumes | 中(需配 pixi v6,老) |
| **@jannchie/pixi-live2d-display(fork)** [npm](https://www.npmjs.com/package/@jannchie/pixi-live2d-display) [源码](https://github.com/Jannchie/pixi-live2d-display) | MIT | **活跃**:latest 1.4.0 (2026-07-17),配 **pixi.js ^8** | ✅ `cubism4.js` bundle + `live2dcubismcore.min.js`;URL 后缀自动识别 `.model3.json`(Cubism3/4)与 `.model.json`(Cubism2) | ✅ **内置** `model.speak(base64Audio)` 自动口型(内部 AnalyserNode 音量→LipSync 参数)、`startMicrophoneLipSync()`、手动 `setLipSyncValue(v)` | **低**(单页原生 JS 直接可用) |
| **Live2D Cubism Web SDK(官方)** [下载页](https://www.live2d.com/en/sdk/download/web/) [许可](https://www.live2d.com/en/sdk/license/) | 专有许可:**开发免费,发布需按计划签约付费**(个人/小型企业豁免;VTube 追踪软件属 Expandable Applications,发布前需审查+签约)| 活跃(SDK for Web 当前 5.x,2026-08 已出 5.4 alpha;CubismWebSamples 仓库 2026-04 仍在更新) | ✅ Cubism3/4/5;**不支持 Cubism 2.1 模型**(官方明确 moc3/motion3.json 与 2.1 格式互不兼容) | 框架含 LipSync 参数机制,但**无音频播放/分析内置**,官方样例用麦克风或自写;另有 MotionSync 插件(独立许可) | **高**(下载 ZIP 需 Live2D 账号+许可协议;Core 不上 GitHub/Core 不发布 npm;需手写 WebGL 渲染循环与资源加载),框架代码在 GitHub ([CubismWebSamples](https://github.com/Live2D/CubismWebSamples)) |
| **live2d-widget**(stevenjoezhang) [仓库](https://github.com/stevenjoezhang/live2d-widget) | **GPL-3.0** | 活跃(2026-09 仍有提交,10.9k stars) | 以 Cubism 2.1 模型为主(Cubism 2.1 Core 官方已停下载,靠 jsdelivr 镜像) | 为"网页挂件"场景设计,音频口型需自行扩展 | 低,但定位不符(GPL 传染、非交互主体设计) |
| **自封装**(裸 `live2dcubismcore` wasm + 自绘) | 同官方内核许可 | n/a | ✅ | 全自写 | 极高,不推荐 |

**Cubism Core 获取要点**(三种走 pixi 路线都绕不开):
- Cubism3+ 必须 `live2dcubismcore.min.js`:官方不发布 GitHub、不发布 npm(仅有 2022 年的社区镜像包 `live2dcubismcore@1.0.2`,不建议依赖);官方建议从 SDK 包内取出;官方直链 `https://cubism.live2d.com/sdk-web/cubismcore/live2dcubismcore.min.js` **可用但不稳定,不用于生产**([原版 README 原话](https://github.com/guansss/pixi-live2d-display))。**本项目已有该文件**(`frontend/libs/live2dcubismcore.min.js`,206KB),直接随静态资源分发即可,无需再下载。
- Cubism 2.1 模型需旧 Core `live2d.min.js`(官方 2019-09-04 起停止提供,[原因公告](https://help.live2d.com/en/other/other_20/);镜像在 [dylanNew/live2d](https://github.com/dylanNew/live2d) + jsdelivr)。本项目无 Cubism2 资产,**不需要**。

---

## 3. 推荐方案

**主选:`@jannchie/pixi-live2d-display` 1.4.x + pixi.js 8.x + 本地 `live2dcubismcore.min.js`(cubism4 bundle 即可;assets 全为 Cubism3,无需 cubism2/live2d.min.js)。**

理由:
1. **资产 100% 兼容**:两个模型都是 Cubism3,fork 的 Cubism4 运行时直读 `.model3.json`,URL 自动判别格式,零适配。
2. **口型同步零成本**:`model.speak(base64WavDataUrl, {volume, expression, resetExpression})` 一条调用同时完成「解码→播放→音量分析→LipSync 参数驱动」,且自动读取 model3.json 的 `Groups.LipSync`(mao_pro 用 `ParamA`、shizuku 用 `PARAM_MOUTH_OPEN_Y`,前端无需感知差异)。表情也可随 speak 一起应用并播完复位;另有 `setLipSyncValue(v)`(0=闭,1=开)供手动/服务端 volumes 路线兜底。与原版需要手写 AnalyserNode 循环相比,这是决定性的省码点。
3. **活跃维护、MIT**:1.4.0 发布于 2026-07,紧随 pixi v8;原版已停更两年多,不可选。
4. **集成成本最低**:原生 JS 单页 + 两个 CDN/本地 script 即可(`pixi.js@8` + fork 的 `dist/cubism4.min.js`),无打包器诉求;符合"极简 HTML+Live2D"目标。
5. 现有的 `live2dcubismcore.min.js` 资产直接复用,不经第三方 CDN,规避官方直链不稳定问题与许可争议。

**注意事项 / 降级路线**:
- fork 的 `speak()` 每次调用会停掉上一段语音(`isSpeaking → stopSpeaking`),连续多句需前端排队或等后端回执;前端应实现一个简单 FIFO 播放队列。
- iOS/Safari:AudioContext 需在用户手势中创建/resume,页面必须有"点击开始"交互(直播场景天然满足)。
- 表情切换:`model.expression(索引|名称)`,若模型无 Expressions(shizuku 即无),`ExpressionManager` 不存在,调用需判空守卫(后端也不应对 shizuku 下发表情)。
- 若日后要兼容 Cubism 2.1 资产,换 `index.js` bundle + 加引 `live2d.min.js` 即可,一行配置的事。

---

## 4. 关键 API 与示例锚点

### 4.1 最小加载+口型+表情(伪代码,基于 fork 1.4.x)

```html
<script src="/libs/pixi.js@8/dist/pixi.min.js"></script>
<script src="/libs/live2dcubismcore.min.js"></script>
<script src="/libs/pixi-live2d-display/dist/cubism4.min.js"></script>
<!-- 全局命名空间: PIXI.live2d.Live2DModel -->
```

```js
const app = new PIXI.Application();
await app.init({ canvas, resizeTo: window, backgroundAlpha: 0 }); // pixi v8 异步初始化

const model = await PIXI.live2d.Live2DModel.from('/live2d-models/mao_pro/runtime/mao_pro.model3.json', {
  ticker: PIXI.Ticker.shared,          // 自动更新
  motionPreload: PIXI.live2d.MotionPreloadStrategy.IDLE,
  autoInteract: true,
});
app.stage.addChild(model);

// 口型 + 表情 一条龙(核心锚点):
await model.speak('data:audio/wav;base64,' + wavBase64, {
  volume: 1,
  expression: 3,          // 对应 exp_04,即 emo_map 的 joy/surprise
  resetExpression: true,  // 播完自动复位到 neutral(0)
});

// 纯表情指令(无音频):
model.expression(0); // 按索引; 也可 model.expression('exp_01'); model.expression() = 随机

// 动作:
model.motion('Idle', 0, PIXI.live2d.MotionPriority.NORMAL);

// 手动口型(服务端 volumes 路线兜底):
model.startLipSync();  model.setLipSyncValue(0.8);  model.stopLipSync();
```

### 4.2 服务端 volumes 路线(旧协议兼容,不推荐但可行)

若走"后端算 RMS 数组"老路:前端 rAF 内按播放时间取 `volumes[i]`,调 `model.internalModel.setLipSyncValue(v)`(fork 封装)或 `internalModel.coreModel.setParameterValueById(lipSyncId, v)`(原版路径)。Go 端无需此,推荐 4.1 客户端分析。

### 4.3 Web Audio 播放 base64 wav 注意点(无论走哪条路都适用)

1. **decodeAudioData 会 detach 输入 ArrayBuffer**:换行/重播必须传新 buffer,同一 base64 需每播一次重新解码一次(或缓存 AudioBuffer)。
2. base64 解码注意字节偏移:`atob`→`Uint8Array` 后直接取 `.buffer` 可能带非零 byteOffset,需 `new Uint8Array(bytes.buffer.slice(byteOffset, byteOffset+length))` 再 decode(fork 的 AudioAnalyzer 已处理,自写时要小心)。
3. **iOS Safari**:AudioContext 默认 suspended,须在用户手势里 `ctx.resume()`;`decodeAudioData` promise 形式 14.1+ 支持。
4. 采样率无需手动处理:decodeAudioData 自动重采样到 context 采样率(如 TTS 24kHz→48kHz)。
5. 尺寸:base64 比原始字节膨胀 ~33% 且 JSON 解析再翻一份内存;数秒 TTS wav(几十~几百 KB)没问题,超长/流式音频应改 WS 二进制帧(PCM 或 wav 字节)。
6. `AudioBufferSourceNode` 一次性,连播多句用 `ctx.currentTime` 链式 `start()` 调度或等 `onended`;调用 fork `speak()` 时它内部自动 stop 上一段,前端排队即可。

### 4.4 主要参考链接

- [@jannchie/pixi-live2d-display(推荐,含 speak/口型文档)](https://www.npmjs.com/package/@jannchie/pixi-live2d-display) · [源码/中文 README](https://github.com/Jannchie/pixi-live2d-display)
- [原版 pixi-live2d-display(停更基线与 Cubism Core 说明)](https://github.com/guansss/pixi-live2d-display) · [npm](https://www.npmjs.com/package/pixi-live2d-display)
- [Cubism SDK 官方下载页(Web,需账号+许可协议)](https://www.live2d.com/en/sdk/download/web/) · [SDK 发布许可/收费计划(含 AI/Chatbot 与 VTube Expandable 分类)](https://www.live2d.com/en/sdk/license/)
- [Cubism Core 说明(核心不上 GitHub、包含于 SDK 包)](https://docs.live2d.com/en/cubism-sdk-manual/cubism-core/) · [与 Cubism 2.1 SDK 的差异(格式互不兼容的官方声明)](https://docs.live2d.com/en/cubism-sdk-manual/changefrom21/)
- [Cubism 2.1 停止下载公告](https://help.live2d.com/en/other/other_20/) · [Cubism 2.1 核心镜像 dylanNew/live2d](https://github.com/dylanNew/live2d)
- [live2d-widget(GPL-3.0,挂件场景)](https://github.com/stevenjoezhang/live2d-widget)

---

## 5. 给 Go 后端 `/client-ws` 的协议设计建议

1. **消息信封与类型**(对齐现有 Python 载荷,便于迁移):文本 JSON 帧定死信封 `{"type": ..., "seq": <递增id>, "ts": ms, "payload": {...}}`;下行音频类型建议:
   - `type:"audio"`: `{audio: <base64 wav>, expression: <int|null>, reset_expression: bool, display_text, motion_group?, motion_index?}` — 对应前端一句 `speak()`,表情与音频原子绑定,天然避免时序竞态;
   - `type:"expression"`: `{index}` 纯表情(无音频场景,如直播间投喂/角色事件);
   - `type:"motion"` / `type:"model"`(切换模型,下发 `model_path`) / `type:"ping"`。
   `expression` 语义沿用 model_dict.json 的 **emo_map 标签→表达式下标**(Go 端解析 LLM 输出里的 `[joy]` 等标签、查表、并剥标签后再下发 display_text),前端只负责数值落地。
2. **口型数据不必再随音频下发**:改用 fork `speak()` 客户端分析(音量→LipSync 组参数),彻底删除旧协议的 `volumes[]` + `slice_length`(减少 30%+ 载荷与前端时序代码);若坚持服务端 RMS,可在 `audio` 消息里保留 `volumes` 字段作兜底,前端二选一。音频格式:Go TTS 输出统一编码成 **WAV(PCM16, 24kHz mono)** 再 base64,与现 Python 行为一致;后续要极致低延迟再演进为二进制帧送裸 PCM + `sample_rate/channels` 元数据。
3. **客户端回执(上行)闭环**:`audio_started(id)` / `audio_finished(id)` / `error{code,msg}`。理由:fork `speak()` 单飞(新 speak 会停旧 speak),后端流式 TTS 必须知道上一句播完才能安全推下一句;同时 `audio_finished` 供后端做(可选)播放耗时统计与重试。上行另有 `hello{model}` 与 `model_loaded` 便于后端按模型过滤表情指令(shizuku 无 Expressions,后端不下发)。
4. **模型清单与文件走 HTTP(不进 WS)**:保留原 `/live2d-models/info` 语义(目录扫描 `X.model3.json`,返回 `name/avatar/model_path`),`live2d-models/` 由 Go 静态文件服务挂载;WS 只传指令与音频,不传模型资源。
5. **表情复位策略**:默认 `reset_expression: true`(speak 播完回 neutral),避免表情"卡死"在上一句;需要持续表情(如生气状态)由后端主动发 `expression` 指令覆盖。前端对 `model.expression()` 判空(shizuku 无 ExpressionManager)。

---

### 附:许可提示(非法律意见)

Cubism SDK 开发/试用免费;内容**发布**时个人与小型企业豁免签约付费,**VTube 追踪软件属 Expandable Applications,发布前须向 Live2D 审查签约**;AI/Chatbot 类 Web 应用对应计划 B(非营利)/E(一次性)/G(运行分成)。Go 重写为自用/开源学习项目阶段无影响;若将来作为商业产品发布需按官网流程签约([许可页](https://www.live2d.com/en/sdk/license/))。模型资产 mao_pro/shizuku 为 Live2D 官方示例模型,遵循 Live2D 示例数据条款。