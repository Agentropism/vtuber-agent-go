// SyAgent 前端：连接 /client-ws，收播报、出声、做表情与口型。
//
// 设计要点：
//   - 播报严格串行（promise 链）：服务端等回执才发下一句，前端也必须一句放完再放下一句。
//   - 音频由服务端补好 WAV 头再 base64，这里直接喂给 <audio>，不做任何解码。
//   - 口型交给 pixi-live2d-display 的 speak()：它内部按音量驱动 ParamMouthOpenY。

'use strict';

const statusEl = document.getElementById('status');
const captionEl = document.getElementById('caption');
const speakerEl = document.getElementById('speaker');
const textEl = document.getElementById('text');
const gateEl = document.getElementById('gate');
const gateTitleEl = document.getElementById('gate-title');
const startButton = document.getElementById('start');
const composerEl = document.getElementById('composer');
const inputEl = document.getElementById('message-input');
const sendButton = document.getElementById('send');

const RECONNECT_MIN = 1000;
const RECONNECT_MAX = 15000;

// 本地文本输入的目标渠道与身份：可用 URL 参数覆盖（?channel=group_123456&name=小明）。
// 渠道号前缀决定平台（room_ → B站 / group_ → QQ），默认用本地直播间，不打扰任何真实平台。
const query = new URLSearchParams(location.search);
const CHANNEL = query.get('channel') || 'room_local';
const USER_NAME = query.get('name') || '本地观众';
const LOCAL_USER_ID = 'local';

let app = null;
let model = null;
let socket = null;
let reconnectDelay = RECONNECT_MIN;

// 模型在屏幕上的摆放参数，resize 时按它重新定位
let modelLayout = { scale: 0.5, xShift: 0, yShift: 0 };

// 播放队列：保证同一时刻只有一句在放
let playChain = Promise.resolve();

// ---- 口型同步 ----
// pixi-live2d-display 本身不带 lip sync，这里用 Web Audio 分析音量，
// 再在内部模型更新之后把音量写进口型参数（写在后面才能盖住动作给的值）。
let audioCtx = null;
let mouthLevel = 0;
let mouthParamIds = null;

function ensureAudioContext() {
  if (!audioCtx) {
    const Ctor = window.AudioContext || window.webkitAudioContext;
    audioCtx = new Ctor();
  }
  return audioCtx;
}

// 口型参数优先取模型自己的 LipSync 组，取不到就退回 ParamMouthOpenY
function lipSyncParamIds() {
  if (mouthParamIds) return mouthParamIds;

  mouthParamIds = ['ParamMouthOpenY'];
  const groups = (model && model.internalModel && model.internalModel.settings
    && model.internalModel.settings.groups) || [];
  const group = groups.find((g) => (g.Name || g.name) === 'LipSync');
  const ids = group && (group.Ids || group.ids);
  if (ids && ids.length) {
    mouthParamIds = ids;
  }
  return mouthParamIds;
}

function installLipSync() {
  const internal = model && model.internalModel;
  if (!internal || internal.__syagentLipSync) return;

  const original = internal.update.bind(internal);
  internal.update = function (dt, now) {
    original(dt, now);

    const core = internal.coreModel;
    if (!core) return;
    for (const id of lipSyncParamIds()) {
      core.setParameterValueById(id, mouthLevel);
    }
  };
  internal.__syagentLipSync = true;
}

// 播放一段 WAV（base64），期间按音量驱动口型，放完 resolve
function playAudio(base64) {
  return new Promise((resolve, reject) => {
    const ctx = ensureAudioContext();
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }

    ctx.decodeAudioData(bytes.buffer, (buffer) => {
      const source = ctx.createBufferSource();
      source.buffer = buffer;

      const analyser = ctx.createAnalyser();
      analyser.fftSize = 1024;
      source.connect(analyser);
      analyser.connect(ctx.destination);

      const samples = new Uint8Array(analyser.fftSize);
      let frame = 0;
      const tick = () => {
        analyser.getByteTimeDomainData(samples);
        let sum = 0;
        for (let i = 0; i < samples.length; i++) {
          const v = (samples[i] - 128) / 128;
          sum += v * v;
        }
        // 乘一个系数，小音量也能看出嘴动
        mouthLevel = Math.min(1, Math.sqrt(sum / samples.length) * 4);
        frame = requestAnimationFrame(tick);
      };
      tick();

      source.onended = () => {
        cancelAnimationFrame(frame);
        mouthLevel = 0;
        resolve();
      };

      source.start();
    }, (err) => reject(err));
  });
}

function setStatus(text, kind) {
  statusEl.textContent = text;
  statusEl.className = kind || '';
}

function showCaption(speaker, text) {
  if (!text) {
    captionEl.classList.remove('on');
    return;
  }
  speakerEl.textContent = speaker ? `${speaker}：` : '';
  textEl.textContent = text;
  captionEl.classList.add('on');
}

// ---- 本地文本输入 ----
// 以观众身份发一条消息：POST /api/sessions/<渠道>/messages。它与平台事件走同一条上传管线
// （去重、敏感词、背压一个不落），因此会进会话、会生成回复、会写长期记忆；
// 回复经播报队列回来，由上面的 speak 流程出声与做表情。
let sending = false;

async function sendLocalMessage() {
  const text = inputEl.value.trim();
  if (!text || sending) return;

  sending = true;
  sendButton.disabled = true;
  try {
    const resp = await fetch(`/api/sessions/${encodeURIComponent(CHANNEL)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: LOCAL_USER_ID, user_name: USER_NAME, text: text }),
    });
    if (!resp.ok) {
      let detail = `HTTP ${resp.status}`;
      try {
        const body = await resp.json();
        if (body && body.error) detail = body.error;
      } catch (err) {
        // 响应体不是 JSON 时保留状态码
      }
      throw new Error(detail);
    }
    inputEl.value = '';
  } catch (err) {
    setStatus(`发送失败：${err.message}`, 'err');
  } finally {
    sending = false;
    sendButton.disabled = false;
    inputEl.focus();
  }
}

async function createStage() {
  const canvas = document.getElementById('stage');
  app = new PIXI.Application({
    view: canvas,
    resizeTo: window,
    backgroundAlpha: 0,
    antialias: true,
    autoStart: true,
  });

  // 虚拟屏上没有 GPU，渲染全靠 CPU（SwiftShader）：不限帧率时 Chrome 以 60fps 满速渲染，
  // 实测吃掉 7 个核，把 ffmpeg 饿到 0.86 倍速（推流被服务端判为涓流后掐断）。
  // 推流本身才 15-20fps，24fps 足够口型跟手。
  app.ticker.maxFPS = 24;
}

async function loadModel(info) {
  if (!info || !info.url) {
    throw new Error('服务端没有下发模型信息');
  }

  if (model) {
    app.stage.removeChild(model);
    model.destroy();
    model = null;
  }

  model = await PIXI.live2d.Live2DModel.from(info.url);
  window.__model = model; // 便于排查模型尺寸与状态
  app.stage.addChild(model);

  modelLayout = {
    // 模型清单里的 scale 读作「相对适配尺寸的倍率」：1.0 = 正好占满视口高度
    userScale: info.scale || 1,
    xShift: info.x_shift || 0,
    yShift: info.y_shift || 0,
  };
  applyLayout();
  installLipSync();

  // 待机动作：模型里有该动作组就一直循环
  if (info.idle_motion) {
    model.motion(info.idle_motion);
    setInterval(() => model && model.motion(info.idle_motion), 8000);
  }
}

// 底部居中摆放，并按视口缩放；窗口尺寸变化时重新定位
function applyLayout() {
  if (!model || !app) return;

  const internal = model.internalModel;
  const fit = Math.min(app.screen.width / internal.width, app.screen.height / internal.height);

  model.scale.set(fit * modelLayout.userScale);
  model.anchor.set(0.5, 1);
  model.x = app.screen.width / 2 + modelLayout.xShift;
  model.y = app.screen.height + modelLayout.yShift;
}
window.addEventListener('resize', applyLayout);

// 播放一段播报：显示字幕 + 出声 + 表情，返回一个在放完后 resolve 的 Promise
function playSegment(data) {
  return new Promise((resolve) => {
    const finish = () => {
      showCaption(null, '');
      send('playback-finished', { seq: data.seq });
      resolve();
    };

    if (!model || !data.audio) {
      // 没有模型或没有音频（合成失败降级为纯字幕）时按字数估个时长
      const ms = data.duration_ms || Math.max(1200, (data.text || '').length * 180);
      setTimeout(finish, ms);
      return;
    }

    // 表情：服务端已经换算成表达式下标，-1 表示不换
    if (data.emotion >= 0) {
      try {
        model.expression(data.emotion);
      } catch (err) {
        console.warn('切换表情失败', err);
      }
    }

    let settled = false;
    const done = () => {
      if (settled) return;
      settled = true;
      clearTimeout(guard);
      if (data.emotion >= 0) {
        model.expression(); // 播完复位，避免表情一直挂着
      }
      finish();
    };

    // 兜底：解码失败或 onended 没触发时，别把播报队列卡死
    const guard = setTimeout(done, (data.duration_ms || 3000) + 5000);

    playAudio(data.audio).then(done).catch((err) => {
      console.error('播放失败', err);
      done();
    });
  });
}

function send(type, data) {
  if (!socket || socket.readyState !== WebSocket.OPEN) return;
  socket.send(JSON.stringify({ type: type, data: data || {} }));
}

function handleMessage(msg) {
  switch (msg.type) {
    case 'hello': {
      const character = (msg.data && msg.data.character) || {};
      speakerEl.dataset.name = character.name || '';
      if (character.name) {
        gateTitleEl.textContent = character.name;
        document.title = `${character.name} · SyAgent`;
      }
      loadModel(msg.data && msg.data.model)
        .then(() => setStatus(`已连接 · ${character.name || ''}`, 'ok'))
        .catch((err) => {
          console.error(err);
          setStatus(`模型加载失败：${err.message}`, 'err');
        });
      break;
    }

    case 'speak': {
      const data = msg.data || {};
      playChain = playChain
        .then(() => {
          showCaption(speakerEl.dataset.name, data.text);
          send('playback-started', { seq: data.seq });
          return playSegment(data);
        })
        .catch((err) => console.error('播报失败', err));
      break;
    }

    case 'emotion': {
      // 表情预览：服务端已换算成表达式下标，只换表情、不出声
      const data = msg.data || {};
      if (model && data.emotion >= 0) {
        try {
          model.expression(data.emotion);
        } catch (err) {
          console.warn('切换表情失败', err);
        }
      }
      break;
    }

    default:
      console.debug('忽略消息', msg);
  }
}

function connect() {
  const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
  socket = new WebSocket(`${scheme}://${location.host}/api/client-ws`);

  socket.onopen = () => {
    reconnectDelay = RECONNECT_MIN;
    setStatus('已连接，等待模型信息…');
  };

  socket.onmessage = (event) => {
    let msg = null;
    try {
      msg = JSON.parse(event.data);
    } catch (err) {
      console.error('解析消息失败', err);
      return;
    }
    handleMessage(msg);
  };

  socket.onclose = () => {
    setStatus(`连接已断开，${Math.round(reconnectDelay / 1000)} 秒后重连…`, 'err');
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX);
  };

  socket.onerror = () => {
    // onclose 会接着触发，这里不重复处理
  };
}

async function main() {
  await createStage();

  inputEl.placeholder = `对 ${CHANNEL} 说点什么…（回车发送）`;
  composerEl.addEventListener('submit', (event) => {
    event.preventDefault();
    sendLocalMessage();
  });

  // 无人值守推流时没有人能点页面（Chrome 以 --autoplay-policy=no-user-gesture-required
  // 启动），用 ?autostart=1 直接连；正常打开仍然等一次点击，
  // 免得把「开始播报之前」的内容静默丢掉。
  if (query.get('autostart') === '1') {
    // 推流时页面就是画面：输入框不入镜，字幕放回底部
    document.body.classList.add('autostart');
    gateEl.classList.add('off');
    connect();
    return;
  }
  // 等一次用户点击：浏览器的自动播放策略要求先有交互，否则 speak() 会被拒绝。
  // 点击之后才连服务端，这样开始播报之前的内容不会被静默丢掉。
  startButton.addEventListener('click', () => {
    gateEl.classList.add('off');
    composerEl.classList.remove('off');
    setStatus('连接中…');
    connect();
  });
}

main().catch((err) => {
  console.error(err);
  setStatus(`初始化失败：${err.message}`, 'err');
});
