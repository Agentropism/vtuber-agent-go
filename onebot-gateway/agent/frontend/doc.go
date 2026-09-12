// Package frontend 提供自研极简前端托管与 /client-ws 新协议(执行计划 E6)。
//
// 前端:极简 HTML + 原生 JS + @jannchie/pixi-live2d-display + pixi.js 8 +
// 本地 live2dcubismcore.min.js;资产复用 live2d-models/(Cubism 3)。
// 协议:JSON 信封 {type,seq,ts,payload};audio 原子绑定 {audio base64 wav,
// expression, reset_expression, display_text};上行回执 audio_started/finished/error;
// 模型清单走 HTTP(/live2d-models/info 语义保留)。
package frontend
