package bilibili

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"
)

func TestParsePacketsJSON(t *testing.T) {
	body := []byte(`{"cmd":"LIVE_OPEN_PLATFORM_DM","data":{"msg":"你好"}}`)
	raw := encodePacket(packet{version: verJSON, operation: opMessage, body: body})

	packets, err := parsePackets(raw)
	if err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(packets) != 1 {
		t.Fatalf("包数量 = %d, want 1", len(packets))
	}
	if packets[0].operation != opMessage || packets[0].version != verJSON {
		t.Fatalf("包头字段错误: %#v", packets[0])
	}
	if !bytes.Equal(packets[0].body, body) {
		t.Fatalf("正文 = %s, want %s", packets[0].body, body)
	}
}

// 一条 WebSocket 消息里可以拼接多个包。
func TestParsePacketsConcatenated(t *testing.T) {
	first := encodePacket(packet{version: verJSON, operation: opMessage, body: []byte(`{"cmd":"A"}`)})
	second := encodePacket(packet{version: verJSON, operation: opMessage, body: []byte(`{"cmd":"B"}`)})

	packets, err := parsePackets(append(append([]byte{}, first...), second...))
	if err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(packets) != 2 {
		t.Fatalf("包数量 = %d, want 2", len(packets))
	}
	if string(packets[0].body) != `{"cmd":"A"}` || string(packets[1].body) != `{"cmd":"B"}` {
		t.Fatalf("正文顺序错误: %s / %s", packets[0].body, packets[1].body)
	}
}

// version=2 的压缩帧必须被摊平成子包——原 Rust 实现把它整包丢了。
func TestParsePacketsZlib(t *testing.T) {
	inner := append(
		encodePacket(packet{version: verJSON, operation: opMessage, body: []byte(`{"cmd":"A"}`)}),
		encodePacket(packet{version: verJSON, operation: opMessage, body: []byte(`{"cmd":"B"}`)})...,
	)

	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(inner); err != nil {
		t.Fatalf("压缩: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭压缩流: %v", err)
	}

	raw := encodePacket(packet{version: verZlib, operation: opMessage, body: compressed.Bytes()})

	packets, err := parsePackets(raw)
	if err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(packets) != 2 {
		t.Fatalf("包数量 = %d, want 2（压缩帧里的子包必须被摊平）", len(packets))
	}
	if string(packets[0].body) != `{"cmd":"A"}` || string(packets[1].body) != `{"cmd":"B"}` {
		t.Fatalf("子包正文错误: %s / %s", packets[0].body, packets[1].body)
	}
}

func TestParsePacketsRejectsMalformed(t *testing.T) {
	full := encodePacket(packet{version: verJSON, operation: opMessage, body: []byte(`{"cmd":"A"}`)})

	cases := []struct {
		name string
		raw  []byte
	}{
		{"包头不完整", full[:8]},
		{"包头长度异常", func() []byte {
			bad := append([]byte{}, full...)
			binary.BigEndian.PutUint16(bad[4:6], 12)
			return bad
		}()},
		{"包长度超过已收字节", func() []byte {
			bad := append([]byte{}, full...)
			binary.BigEndian.PutUint32(bad[0:4], uint32(len(full)+10))
			return bad
		}()},
		{"包长度超过协议上限", func() []byte {
			bad := append([]byte{}, full...)
			binary.BigEndian.PutUint32(bad[0:4], maxPacketSize+1)
			return bad
		}()},
		{"包长度小于包头", func() []byte {
			bad := append([]byte{}, full...)
			binary.BigEndian.PutUint32(bad[0:4], 8)
			return bad
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parsePackets(tc.raw); err == nil {
				t.Fatal("畸形帧必须返回错误，不能 panic 也不能静默通过")
			}
		})
	}
}

// 鉴权帧的字节布局是协议硬要求，逐字节钉住。
func TestEncodeAuthFrame(t *testing.T) {
	authBody := `{"roomid":123,"protover":2,"group":"open"}`
	got := encodePacket(packet{version: verPlain, operation: opAuth, body: []byte(authBody)})

	want := make([]byte, headerSize+len(authBody))
	binary.BigEndian.PutUint32(want[0:4], uint32(headerSize+len(authBody)))
	binary.BigEndian.PutUint16(want[4:6], headerSize)
	binary.BigEndian.PutUint16(want[6:8], verPlain)
	binary.BigEndian.PutUint32(want[8:12], opAuth)
	binary.BigEndian.PutUint32(want[12:16], 0)
	copy(want[headerSize:], authBody)

	if !bytes.Equal(got, want) {
		t.Fatalf("鉴权帧字节不符:\n got %v\nwant %v", got, want)
	}
}

// 心跳帧没有 body，整包就是 16 字节包头。
func TestEncodeHeartbeatFrame(t *testing.T) {
	got := encodePacket(packet{version: verPlain, operation: opHeartbeat, sequence: 7})

	if len(got) != headerSize {
		t.Fatalf("心跳帧长度 = %d, want %d", len(got), headerSize)
	}
	if op := binary.BigEndian.Uint32(got[8:12]); op != opHeartbeat {
		t.Fatalf("operation = %d, want %d", op, opHeartbeat)
	}
	if seq := binary.BigEndian.Uint32(got[12:16]); seq != 7 {
		t.Fatalf("sequence = %d, want 7", seq)
	}
}
