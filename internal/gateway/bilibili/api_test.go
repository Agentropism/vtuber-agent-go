package bilibili

import (
	"strings"
	"testing"
)

// 签名串的布局是踩过坑的地方（历史上因为多行字符串字面量带缩进，签名整体算错），
// 用固定向量把「六行、LF 分隔、无缩进、无尾随换行」钉死。
func TestSignSourceLayout(t *testing.T) {
	got := signSource(
		"test-access-key",
		"d41d8cd98f00b204e9800998ecf8427e",
		"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"1700000000",
	)

	want := "x-bili-accesskeyid:test-access-key\n" +
		"x-bili-content-md5:d41d8cd98f00b204e9800998ecf8427e\n" +
		"x-bili-signature-method:HMAC-SHA256\n" +
		"x-bili-signature-nonce:f47ac10b-58cc-4372-a567-0e02b2c3d479\n" +
		"x-bili-signature-version:1.0\n" +
		"x-bili-timestamp:1700000000"

	if got != want {
		t.Fatalf("签名串不符：\n got %q\nwant %q", got, want)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("签名串不能有尾随换行")
	}
	if strings.Contains(got, " ") || strings.Contains(got, "\t") {
		t.Fatal("签名串里不能出现空白（历史 bug：源码缩进混进了签名）")
	}
}

// 期望值由独立的 Python 实现算出，用来交叉验证 HMAC-SHA256 与小写 hex。
func TestSign(t *testing.T) {
	source := signSource(
		"test-access-key",
		"d41d8cd98f00b204e9800998ecf8427e",
		"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"1700000000",
	)

	got := sign("test-secret", source)
	want := "1a71ef74587748c22c9f42dd82cf34b9b2bcf4737f2fcf7a2cd1a7267f7fe02a"
	if got != want {
		t.Fatalf("签名 = %s, want %s", got, want)
	}
}

func TestSignHeaders(t *testing.T) {
	client, err := New(Config{
		AccessKey:       "test-access-key",
		AccessKeySecret: "test-secret",
		IDCode:          "code",
		AppID:           1,
	}, func([]byte) {})
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}

	headers := client.signHeaders([]byte(`{"code":"abc","app_id":123}`))

	// md5 覆盖的是实际发出的 body 字节。
	if got := headers["x-bili-content-md5"]; got != "0e20164c7be0dfad848dd5b5f67089b0" {
		t.Fatalf("content-md5 = %s", got)
	}
	if headers["x-bili-signature-method"] != "HMAC-SHA256" || headers["x-bili-signature-version"] != "1.0" {
		t.Fatalf("签名方法/版本头错误: %#v", headers)
	}
	if len(headers["Authorization"]) != 64 {
		t.Fatalf("Authorization 长度 = %d, want 64", len(headers["Authorization"]))
	}
	if strings.ToLower(headers["Authorization"]) != headers["Authorization"] {
		t.Fatal("Authorization 必须是小写 hex")
	}
	if headers["x-bili-signature-nonce"] == "" || headers["x-bili-timestamp"] == "" {
		t.Fatalf("nonce / timestamp 不能为空: %#v", headers)
	}
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"缺少 access_key", Config{AccessKeySecret: "s", IDCode: "c", AppID: 1}},
		{"缺少密钥", Config{AccessKey: "k", IDCode: "c", AppID: 1}},
		{"缺少身份码", Config{AccessKey: "k", AccessKeySecret: "s", AppID: 1}},
		{"缺少 app_id", Config{AccessKey: "k", AccessKeySecret: "s", IDCode: "c"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg, func([]byte) {}); err == nil {
				t.Fatal("配置不完整必须报错")
			}
		})
	}
}

func TestNewRejectsNilHandler(t *testing.T) {
	_, err := New(Config{AccessKey: "k", AccessKeySecret: "s", IDCode: "c", AppID: 1}, nil)
	if err == nil {
		t.Fatal("handler 为空必须报错")
	}
}

func TestNonceLooksLikeUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		nonce := newNonce()
		if len(nonce) != 36 || strings.Count(nonce, "-") != 4 {
			t.Fatalf("nonce 形态不对: %s", nonce)
		}
		if nonce[14] != '4' {
			t.Fatalf("nonce 不是 v4: %s", nonce)
		}
		if seen[nonce] {
			t.Fatalf("nonce 重复: %s", nonce)
		}
		seen[nonce] = true
	}
}
