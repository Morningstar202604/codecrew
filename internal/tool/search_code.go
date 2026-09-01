package tool

import (
	"context"
	"fmt"

	"codecrew/internal/knowledge"
)

// SearchCodeTool 让模型搜索代码库，返回相关代码片段。
type SearchCodeTool struct {
	searcher *knowledge.Searcher
}

// NewSearchCodeTool 创建代码搜索工具。
func NewSearchCodeTool(searcher *knowledge.Searcher) *SearchCodeTool {
	return &SearchCodeTool{searcher: searcher}
}

func (t *SearchCodeTool) Name() string { return "search_code" }

func (t *SearchCodeTool) Description() string {
	return "搜索代码库，根据关键词或自然语言查询返回相关的代码片段和文件位置。在需要了解项目结构、查找函数实现、定位问题时使用"
}

func (t *SearchCodeTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"query":  stringSchema("搜索查询，可以是关键词、函数名、类名或自然语言描述"),
		"limit":  integerSchema("返回结果数量，默认 5，最多 20"),
		"symbol": stringSchema("可选，按符号名精确搜索（函数/类型/类名）"),
	}, []string{"query"})
}

func (t *SearchCodeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.searcher == nil {
		return "", fmt.Errorf("代码搜索未初始化")
	}

	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query 不能为空")
	}

	limit := 5
	if l, ok := args["limit"].(float64); ok && int(l) > 0 {
		limit = int(l)
	}
	if limit > 20 {
		limit = 20
	}

	var results []knowledge.SearchResult

	// 如果指定了 symbol，按符号搜索
	if symbol, ok := args["symbol"].(string); ok && symbol != "" {
		results = t.searcher.SearchBySymbol(symbol, limit)
	} else {
		results = t.searcher.Search(query, limit)
	}

	if len(results) == 0 {
		return fmt.Sprintf("没有找到与 %q 匹配的代码。尝试使用更通用的关键词，或先运行 /index 构建索引。", query), nil
	}

	return knowledge.FormatResults(results), nil
}
