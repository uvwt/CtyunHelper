package clink

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
)

const initialPayloadBase64 = "UkVEUQIAAAACAAAAGgAAAAAAAAABAAEAAAABAAAAEgAAAAkAAAAECAAA"

var redqMagic = []byte{'R', 'E', 'D', 'Q'}

type ProxyHandshake struct {
	Type       int    `json:"type"`
	SSL        int    `json:"ssl"`
	Host       string `json:"host"`
	Port       string `json:"port"`
	CA         string `json:"ca"`
	Cert       string `json:"cert"`
	Key        string `json:"key"`
	ServerName string `json:"servername"`
	OQS        int    `json:"oqs"`
}

func NewProxyHandshake(clinkHost, targetHost, targetPort, ca, cert, key string) ProxyHandshake {
	host, port := splitHostPort(clinkHost)
	return ProxyHandshake{
		Type:       1,
		SSL:        1,
		Host:       host,
		Port:       port,
		CA:         ca,
		Cert:       cert,
		Key:        key,
		ServerName: targetHost + ":" + targetPort,
		OQS:        0,
	}
}

func (p ProxyHandshake) JSON() ([]byte, error) {
	return json.Marshal(p)
}

func InitialPayload() []byte {
	value, err := base64.StdEncoding.DecodeString(initialPayloadBase64)
	if err != nil {
		panic(err)
	}
	return value
}

func IsREDQ(data []byte) bool {
	return len(data) >= 4 && data[0] == redqMagic[0] && data[1] == redqMagic[1] && data[2] == redqMagic[2] && data[3] == redqMagic[3]
}

type Message struct {
	Type uint16
	Data []byte
}

// Marshal 按 Clink 的 Type(uint16 LE) + Size(uint32 LE) + Data 编码。
// buildMessage=true 时，在 Data 前再写 dataLength 和固定偏移 8。
func (m Message) Marshal(buildMessage bool) []byte {
	extra := 0
	if buildMessage {
		extra = 8
	}
	buf := make([]byte, 6+extra+len(m.Data))
	binary.LittleEndian.PutUint16(buf[0:2], m.Type)
	binary.LittleEndian.PutUint32(buf[2:6], uint32(extra+len(m.Data)))
	if buildMessage {
		binary.LittleEndian.PutUint32(buf[6:10], uint32(len(m.Data)))
		binary.LittleEndian.PutUint32(buf[10:14], 8)
	}
	copy(buf[6+extra:], m.Data)
	return buf
}

func ParseMessages(buf []byte) ([]Message, error) {
	var result []Message
	for offset := 0; offset < len(buf); {
		if len(buf)-offset < 6 {
			for _, value := range buf[offset:] {
				if value != 0 {
					return result, fmt.Errorf("clink: 尾部残留 %d 字节不足消息头", len(buf)-offset)
				}
			}
			break
		}
		typeID := binary.LittleEndian.Uint16(buf[offset : offset+2])
		size := int(binary.LittleEndian.Uint32(buf[offset+2 : offset+6]))
		if size < 0 || size > len(buf)-offset-6 {
			return result, fmt.Errorf("clink: 消息 type=%d 声明长度 %d 超出剩余数据", typeID, size)
		}
		data := append([]byte(nil), buf[offset+6:offset+6+size]...)
		result = append(result, Message{Type: typeID, Data: data})
		offset += 6 + size
	}
	return result, nil
}

func BuildUserInfoMessage(userID int64, userName string) ([]byte, error) {
	payload, err := json.Marshal(struct {
		Type     int    `json:"type"`
		UserName string `json:"userName"`
		UserInfo string `json:"userInfo"`
		UserID   int64  `json:"userId"`
	}{
		Type:     1,
		UserName: userName,
		UserInfo: "",
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}
	return (Message{Type: 118, Data: payload}).Marshal(true), nil
}

func splitHostPort(value string) (string, string) {
	value = strings.TrimSpace(value)
	if index := strings.LastIndex(value, ":"); index > 0 && !strings.Contains(value[index+1:], ":") {
		return value[:index], value[index+1:]
	}
	return value, "443"
}
