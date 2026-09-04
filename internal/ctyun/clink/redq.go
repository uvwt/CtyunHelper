package clink

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
)

const (
	redqKeyLen  = 128
	redqHashLen = 20
)

// BuildREDQResponse 生成服务端 REDQ 校验帧的响应。
// 该流程等价于 CtYun 第三方实现中的 RSA-OAEP 风格校验，但按协议语义重新实现。
func BuildREDQResponse(challenge []byte) ([]byte, error) {
	return buildREDQResponse(challenge, rand.Reader)
}

func buildREDQResponse(challenge []byte, random io.Reader) ([]byte, error) {
	if !IsREDQ(challenge) {
		return nil, fmt.Errorf("clink: 不是 REDQ 校验帧")
	}
	// 第三方实现先跳过 16 字节，再从相对 32..160 取 129 字节模数、163..165 取指数。
	if len(challenge) < 182 {
		return nil, fmt.Errorf("clink: REDQ 校验帧过短: %d", len(challenge))
	}
	modulus := new(big.Int).SetBytes(challenge[48:177])
	exponent := int(challenge[179])<<16 | int(challenge[180])<<8 | int(challenge[181])
	if modulus.Sign() <= 0 || exponent <= 1 {
		return nil, fmt.Errorf("clink: REDQ RSA 公钥无效")
	}

	seed := make([]byte, redqHashLen)
	if _, err := io.ReadFull(random, seed); err != nil {
		return nil, fmt.Errorf("clink: 生成 REDQ seed: %w", err)
	}

	dbLen := redqKeyLen - redqHashLen - 1
	db := make([]byte, dbLen)
	emptyHash := sha1.Sum(nil)
	copy(db, emptyHash[:])
	// 与现有 Clink 行为保持一致：空消息分隔符位于倒数第二字节。
	db[len(db)-2] = 1

	dbMask := mgf1SHA1(seed, dbLen)
	for i := range db {
		db[i] ^= dbMask[i]
	}
	seedMask := mgf1SHA1(db, redqHashLen)
	for i := range seed {
		seed[i] ^= seedMask[i]
	}

	em := make([]byte, redqKeyLen)
	copy(em[1:1+redqHashLen], seed)
	copy(em[1+redqHashLen:], db)

	message := new(big.Int).SetBytes(em)
	cipher := new(big.Int).Exp(message, big.NewInt(int64(exponent)), modulus).Bytes()
	if len(cipher) > redqKeyLen {
		return nil, fmt.Errorf("clink: REDQ RSA 密文长度异常: %d", len(cipher))
	}
	response := make([]byte, 4+redqKeyLen)
	binary.LittleEndian.PutUint32(response[:4], 1)
	copy(response[4+redqKeyLen-len(cipher):], cipher)
	return response, nil
}

func mgf1SHA1(seed []byte, length int) []byte {
	mask := make([]byte, 0, length)
	var counter uint32
	for len(mask) < length {
		var value [4]byte
		binary.BigEndian.PutUint32(value[:], counter)
		hash := sha1.New()
		hash.Write(seed)
		hash.Write(value[:])
		mask = append(mask, hash.Sum(nil)...)
		counter++
	}
	return mask[:length]
}
