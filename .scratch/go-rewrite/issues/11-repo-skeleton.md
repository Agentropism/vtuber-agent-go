Type: grilling
Status: resolved

## Question

onebot-gateway 仓库内演进的项目骨架决策:包布局(现有 cmd/onebot-gateway + internal/app|event|upload|filter|action|server|config 的保留/重构/删除);llm-vup-bridge distillery 逻辑并入位置(新包?);新增包:conversation、tts/、asr/、broadcast、frontend(静态托管)、bilibili Go WSS 客户端、memory;单二进制入口与模块边界(可单测);旧仓库代码搬迁规则(复制而非跨仓库引用)。

## 进度：0%

下一步：grilling 用户确认骨架与搬迁清单。