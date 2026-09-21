package app

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件把「core 是业务与领域能力，backend 是传输与接入」这条边界写成可执行的检查。
//
// 规则（对应决议：core 不承担服务端职责）：
//  1. core 不得 import 任何 internal/backend 包（反向依赖会形成环，也让 core 无法脱离 HTTP 复用）
//  2. core 不得 import WebSocket 库（本项目唯一的 WS 库就是服务端能力）
//  3. core 的源码里不得出现 HTTP 服务端标识符（起服务、注册路由、托管文件）
//
// 允许的：出站 HTTP 客户端（tts 调云引擎、stream 调 B 站 web 接口）——那是业务能力，不是暴露面。

const coreImportPrefix = "github.com/Agentropism/vtuber-agent-go/internal/core"

const backendImportPrefix = "github.com/Agentropism/vtuber-agent-go/internal/backend"

// coreForbiddenImports 是 core 里禁止出现的直接依赖。
var coreForbiddenImports = []string{
	"github.com/coder/websocket",
}

// coreForbiddenMarkers 是 core 源码里禁止出现的服务端标识符。
var coreForbiddenMarkers = []string{
	"http.Serve(",
	"http.ServeMux",
	"http.Server{",
	"http.ListenAndServe",
	"http.HandleFunc",
	"http.Handle(",
	"http.FileServer",
	"websocket.Accept",
}

// TestCoreBoundary 检查 core 的依赖方向与职责边界。
func TestCoreBoundary(t *testing.T) {
	root := moduleRoot(t)

	packages := listPackages(t, root)
	corePackages := 0

	for importPath, imports := range packages {
		if !strings.HasPrefix(importPath, coreImportPrefix) {
			continue
		}
		corePackages++

		for _, dep := range imports {
			if strings.HasPrefix(dep, backendImportPrefix) {
				t.Errorf("%s 反向依赖 %s：core 不该知道 backend", importPath, dep)
			}
			for _, forbidden := range coreForbiddenImports {
				if dep == forbidden {
					t.Errorf("%s 依赖了 %s：传输层能力属于 backend", importPath, forbidden)
				}
			}
		}
	}

	// 断言真的扫到了 core：否则包前缀写错时这条测试会永远「通过」
	if corePackages == 0 {
		t.Fatalf("没有扫到任何 %s/... 的包，边界检查形同虚设", coreImportPrefix)
	}

	checkCoreSources(t, filepath.Join(root, "internal", "core"))
}

// moduleRoot 返回模块根目录（go.mod 所在）。
func moduleRoot(t *testing.T) string {
	t.Helper()

	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("取 GOMOD: %v", err)
	}

	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatal("当前目录不在 Go module 内")
	}

	return filepath.Dir(gomod)
}

// listPackages 用 go list 取包到直接依赖的映射（不含测试依赖，测试桩不受此约束）。
func listPackages(t *testing.T, root string) map[string][]string {
	t.Helper()

	cmd := exec.Command("go", "list", "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./internal/...")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	packages := make(map[string][]string)

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		importPath, deps, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		packages[importPath] = strings.Fields(deps)
	}

	return packages
}

// checkCoreSources 扫 core 的非测试源码，禁止出现服务端标识符。
func checkCoreSources(t *testing.T, dir string) {
	t.Helper()

	files := 0

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}

		for _, marker := range coreForbiddenMarkers {
			if strings.Contains(string(data), marker) {
				t.Errorf("core/%s 出现服务端标识符 %q：起服务与注册路由属于 backend", rel, marker)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("遍历 core 源码: %v", err)
	}

	if files == 0 {
		t.Fatalf("%s 下没有扫到源码，边界检查形同虚设", dir)
	}
}
