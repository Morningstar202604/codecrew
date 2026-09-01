// Package i18n 提供多语言支持，当前内置中文和英文。
// 翻译采用 key-value 字典，未找到翻译时返回 key 本身（兜底）。
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Language 支持的语言代码。
type Language string

const (
	ZhCN Language = "zh-CN" // 简体中文
	EnUS Language = "en-US" // 美式英语
)

// Parse 解析语言代码，不区分大小写和分隔符。
// 未知值默认中文。
func Parse(s string) Language {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "en", "en-us", "en_us", "english":
		return EnUS
	case "zh", "zh-cn", "zh_cn", "chinese", "":
		return ZhCN
	default:
		return ZhCN
	}
}

// String 返回语言代码字符串。
func (l Language) String() string { return string(l) }

// Dictionary 是一个语言的翻译字典。
type Dictionary map[string]string

// dictionaries 是内置的翻译字典。
var dictionaries = map[Language]Dictionary{
	ZhCN: zhCN,
	EnUS: enUS,
}

// T 返回指定语言的翻译。如果翻译不存在，返回 key 本身。
// args 可选，用于 fmt.Sprintf 格式化。
func T(lang Language, key string, args ...any) string {
	dict, ok := dictionaries[lang]
	if !ok {
		dict = dictionaries[ZhCN]
	}
	text, ok := dict[key]
	if !ok {
		// 兜底：返回 key 本身
		text = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}

// LoadCustom 从 JSON 文件加载自定义翻译，覆盖内置字典。
// 文件格式：{"key": "翻译", ...}
func LoadCustom(lang Language, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var custom map[string]string
	if err := json.Unmarshal(data, &custom); err != nil {
		return err
	}
	dict := dictionaries[lang]
	if dict == nil {
		dict = Dictionary{}
	}
	for k, v := range custom {
		dict[k] = v
	}
	dictionaries[lang] = dict
	return nil
}

// AllKeys 返回指定语言的所有翻译 key（用于调试）。
func AllKeys(lang Language) []string {
	dict, ok := dictionaries[lang]
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}
	return keys
}
