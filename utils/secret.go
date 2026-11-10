package utils

import (
	"encoding/base64"
	"strings"
)

// 表内凭据的轻量可逆混淆。
//
// 注意：这是「混淆」不是「加密」。目的只是让数据库被随手打开时不会一眼看到
// Clash / Mihomo API 口令的明文。secret 只是本机 / 局域网的 API 口令，不是高价值
// 凭据，因此不引入密钥文件、外部主密钥或加密库：拿到源码 + 数据库的人可以还原。
// 真正的防护边界是「文件权限 + 不回显」，而不是密码学强度。

// obfuscateKey 是编译进二进制的固定 key（结构性常量，不随环境变化）。
// 带版本前缀，方便识别格式、避免把任意字符串误当成混淆值还原。
const obfuscatePrefix = "ob1:"

var obfuscateKey = []byte("clash-statistics/credential/v1")

// ObfuscateSecret 把明文口令做可逆混淆，返回可安全存进数据库的字符串。
// 空明文返回空串（表示「未配置」）。
func ObfuscateSecret(plain string) string {
	if plain == "" {
		return ""
	}
	data := []byte(plain)
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ obfuscateKey[i%len(obfuscateKey)]
	}
	return obfuscatePrefix + base64.StdEncoding.EncodeToString(out)
}

// DeobfuscateSecret 还原 ObfuscateSecret 的结果。
// 空串 / 无版本前缀 / 非法 base64 一律返回空串（视为未配置）。
func DeobfuscateSecret(obfuscated string) string {
	if !strings.HasPrefix(obfuscated, obfuscatePrefix) {
		return ""
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(obfuscated, obfuscatePrefix))
	if err != nil {
		return ""
	}
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ obfuscateKey[i%len(obfuscateKey)]
	}
	return string(out)
}
