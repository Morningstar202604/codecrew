package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Provider 描述一个 OpenAI 兼容的上游。

func MaskKey(key string) string {
	switch {
	case key == "":
		return "(未填写)"
	case len(key) <= 10:
		return key[:2] + "****"
	default:
		return key[:6] + "****" + key[len(key)-4:]
	}
}

// WorkDir 返回工具使用的工作目录（用户项目根），默认当前目录。
func (c *Config) WorkDir() string {
	if c.WorkingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "."
		}
		return wd
	}
	abs, err := filepath.Abs(c.WorkingDir)
	if err != nil {
		return c.WorkingDir
	}
	return abs
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// stripComments 去掉 JSON 中的 // 行注释（便于配置模板加说明），字符串内的 // 会被保留。
func stripComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for _, line := range strings.Split(string(data), "\n") {
		out = append(out, []byte(stripLineComment(line))...)
		out = append(out, '\n')
	}
	return out
}

func stripLineComment(line string) string {
	inString := false
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		switch {
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && c == '/' && i+1 < len(line) && line[i+1] == '/':
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}
