Type: grilling
Status: resolved

## Question

单配置文件键面设计决策:合并 conf.yaml(live/agent/stateless_llm/tts/asr/vad/character/proactive_speak)+ config.toml(memory/server/clients/bilibili)+ llm-vup-bridge config.json(distillery)三套键;格式 TOML + viper(沿用网关);旧→新键名映射表;凭据(API key)存放与 gitignore 规则;config_templates 模板同步策略(对外契约含配置键,Q3 基线)。

## 进度：0%

下一步：grilling 用户确认键面与映射。