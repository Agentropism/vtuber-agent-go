package bilibili

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// 二进制帧布局（大端序）：
//
//	偏移 0  长度 4  packet_len  整包长度（含 16 字节头）
//	偏移 4  长度 2  header_len  固定 16
//	偏移 6  长度 2  version     见 ver* 常量
//	偏移 8  长度 4  operation   见 op* 常量
//	偏移 12 长度 4  sequence_id 客户端心跳自增；服务端下行通常为 0
//	偏移 16 长度 N  body        version=1 时是 JSON，version=2 时是 zlib 流
const (
	headerSize    = 16
	maxBodySize   = 2048
	maxPacketSize = maxBodySize + headerSize // 2064

	opHeartbeat      = 2 // 客户端心跳
	opHeartbeatReply = 3 // 心跳回复
	opMessage        = 5 // 消息推送：11 类事件都走这个 operation
	opAuth           = 7 // 客户端鉴权
	opAuthReply      = 8 // 鉴权回复

	verPlain = 0 // 客户端发出的鉴权帧与心跳帧：无 body
	verJSON  = 1 // 未压缩 JSON
	verZlib  = 2 // zlib 压缩的多包

	// maxInflatedSize 是单个压缩帧解压后的字节上限，防止畸形压缩包撑爆内存。
	maxInflatedSize = 1 << 20
)

// packet 是一个协议包。
type packet struct {
	version   int16
	operation int32
	sequence  int32
	body      []byte
}

// encodePacket 把包编码成大端序字节。
func encodePacket(p packet) []byte {
	buf := make([]byte, headerSize+len(p.body))
	binary.BigEndian.PutUint32(buf[0:4], uint32(headerSize+len(p.body)))
	binary.BigEndian.PutUint16(buf[4:6], uint16(headerSize))
	binary.BigEndian.PutUint16(buf[6:8], uint16(p.version))
	binary.BigEndian.PutUint32(buf[8:12], uint32(p.operation))
	binary.BigEndian.PutUint32(buf[12:16], uint32(p.sequence))
	copy(buf[headerSize:], p.body)

	return buf
}

// parsePackets 解析一段字节里的全部包，返回其中的叶子包。
//
// 一条 WebSocket 消息里可以拼接多个包；version=2 的包解压后又是若干子包。
// 这里循环切包、遇到压缩包就递归摊平，调用方拿到的是可以直接派发的事件包。
func parsePackets(buf []byte) ([]packet, error) {
	var packets []packet

	for len(buf) > 0 {
		if len(buf) < headerSize {
			return nil, fmt.Errorf("包头不完整: 只剩 %d 字节", len(buf))
		}

		packetLen := int(binary.BigEndian.Uint32(buf[0:4]))
		headerLen := int(binary.BigEndian.Uint16(buf[4:6]))
		version := int16(binary.BigEndian.Uint16(buf[6:8]))
		operation := int32(binary.BigEndian.Uint32(buf[8:12]))
		sequence := int32(binary.BigEndian.Uint32(buf[12:16]))

		if headerLen != headerSize {
			return nil, fmt.Errorf("包头长度异常: %d", headerLen)
		}
		if packetLen < headerSize || packetLen > maxPacketSize {
			return nil, fmt.Errorf("包长度异常: %d", packetLen)
		}
		// 原实现漏了这一步，包长度大于已收字节时会越界 panic。
		if packetLen > len(buf) {
			return nil, fmt.Errorf("包长度 %d 超过已收字节 %d", packetLen, len(buf))
		}

		body := buf[headerSize:packetLen]

		if version == verZlib {
			inner, err := inflate(body)
			if err != nil {
				return nil, fmt.Errorf("解压消息体: %w", err)
			}
			children, err := parsePackets(inner)
			if err != nil {
				return nil, err
			}
			packets = append(packets, children...)
		} else {
			packets = append(packets, packet{
				version:   version,
				operation: operation,
				sequence:  sequence,
				body:      append([]byte(nil), body...),
			})
		}

		buf = buf[packetLen:]
	}

	return packets, nil
}

// inflate 解压 zlib 流，并限制解压后的体积。
func inflate(body []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	out, err := io.ReadAll(io.LimitReader(reader, maxInflatedSize+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxInflatedSize {
		return nil, fmt.Errorf("解压后超过 %d 字节", maxInflatedSize)
	}

	return out, nil
}
