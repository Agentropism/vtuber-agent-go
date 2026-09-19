// 浏览器端检查：用无头 Chromium 打开 /web/，确认页面真的能跑起来。
//
// 这一步验证的是 JS 侧——模型加载、字幕、点击遮罩、控制台无报错。没有它的话，
// app.js 改坏了只有人工打开浏览器才会发现。
//
// 环境里没有 playwright 时以退出码 0 跳过：静态检查不该因为缺工具而失败。
//
// 环境变量：
//
//	SMOKE_ADDR  网关地址（默认 127.0.0.1:16199）
//	SMOKE_SHOT  截图输出路径（可选；给了就存一张，便于人工看效果）
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const script = `
const { chromium } = require(process.env.PW_MODULE);

(async () => {
  const addr = process.env.SMOKE_ADDR;
  const errors = [];

  let browser;
  try {
    browser = await chromium.launch({ headless: true, args: ['--enable-unsafe-swiftshader', '--no-sandbox'] });
  } catch (err) {
    // 常见情况：playwright 自带的 chromium 版本没装，但缓存里有别的版本，直接指过去
    const fallback = process.env.PW_CHROMIUM;
    if (!fallback) throw err;
    browser = await chromium.launch({
      headless: true,
      executablePath: fallback,
      args: ['--enable-unsafe-swiftshader', '--no-sandbox'],
    });
  }

  const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
  page.on('pageerror', (e) => errors.push('页面异常: ' + e.message));
  page.on('console', (m) => { if (m.type() === 'error') errors.push('控制台错误: ' + m.text()); });

  await page.goto('http://' + addr + '/web/', { waitUntil: 'load' });

  // 点击遮罩之前不该连服务端：没有交互时浏览器会拒绝出声
  const gated = await page.evaluate(() => !document.getElementById('gate').classList.contains('off'));
  if (!gated) errors.push('开始遮罩没有显示');

  await page.click('#start');

  await page.waitForFunction(
    () => document.getElementById('status').textContent.includes('已连接 · '),
    { timeout: 20000 },
  ).catch(() => errors.push('20 秒内没有连上前端通道：' + document.getElementById('status').textContent));

  // 模型渲染出来才算真的可用
  const rendered = await page.evaluate(() => {
    const model = window.__model;
    return !!(model && model.width > 0 && model.height > 0);
  });
  if (!rendered) errors.push('模型没有渲染出来');

  await page.evaluate(async () => {
    await fetch('/inject', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: '浏览器检查：你好呀', emotion: 'joy' }),
    });
  });

  await page.waitForFunction(
    () => document.getElementById('text').textContent.includes('浏览器检查'),
    { timeout: 15000 },
  ).catch(() => errors.push('字幕没有出现'));

  if (process.env.SMOKE_SHOT) {
    await page.screenshot({ path: process.env.SMOKE_SHOT });
  }

  await browser.close();

  if (errors.length > 0) {
    console.error(errors.join('\n'));
    process.exit(1);
  }
  console.log('浏览器检查通过：页面加载 → 点击开始 → 模型渲染 → 字幕显示');
})();
`

func main() {
	module, ok := resolvePlaywright()
	if !ok {
		fmt.Println("跳过浏览器检查（环境里没有 playwright）")
		return
	}

	file, err := os.CreateTemp("", "syagent-browser-*.js")
	if err != nil {
		fmt.Println("跳过浏览器检查（无法写临时脚本）:", err)
		return
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(script); err != nil {
		fmt.Println("跳过浏览器检查（无法写临时脚本）:", err)
		return
	}
	_ = file.Close()

	cmd := exec.CommandContext(context.Background(), "node", file.Name())
	cmd.Env = append(os.Environ(),
		"PW_MODULE="+module,
		"PW_CHROMIUM="+findChromium(),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = filepath.Dir(file.Name())

	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "浏览器检查失败:", err)
		os.Exit(1)
	}
}

// resolvePlaywright 找可用的 playwright 模块；找不到就返回 false。
//
// 先按正常的 node 解析路径找，再退回 @playwright/cli 自带的副本——
// 后者是这台机器上的常见形态（全局装了 cli，playwright 在它自己的 node_modules 里）。
func resolvePlaywright() (string, bool) {
	candidates := []string{"playwright"}

	if root, err := exec.Command("npm", "root", "-g").Output(); err == nil {
		global := strings.TrimSpace(string(root))
		if global != "" {
			candidates = append(candidates, filepath.Join(global, "@playwright", "cli", "node_modules", "playwright"))
		}
	}

	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) {
			// 交给 node 自己解析
			if err := exec.Command("node", "-e", "require('"+candidate+"')").Run(); err == nil {
				return candidate, true
			}
			continue
		}
		if _, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil {
			return candidate, true
		}
	}

	return "", false
}

// findChromium 在 playwright 的浏览器缓存里找一个可用的 chromium。
//
// playwright 自带的版本号可能和缓存里的对不上（工具升级过），这时显式指定
// 可执行文件比让 launch() 直接报错要好。
func findChromium() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	matches, _ := filepath.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium*", "chrome-linux*", "chrome"))
	if len(matches) == 0 {
		return ""
	}

	return matches[len(matches)-1]
}
