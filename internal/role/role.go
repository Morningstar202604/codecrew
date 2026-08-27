package role

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Role struct {
	Name        string
	Description string
	Tools       []string
	Prompt      string
}

func Load(dir string) ([]Role, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	roles := []Role{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		r, err := loadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		roles = append(roles, r)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles, nil
}

func loadFile(path string) (Role, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Role{}, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return Role{}, fmt.Errorf("missing frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Role{}, fmt.Errorf("missing frontmatter end")
	}
	r := Role{}
	for _, line := range lines[1:end] {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			r.Name = value
		case "description":
			r.Description = value
		case "tools":
			value = strings.Trim(value, "[]")
			for _, tool := range strings.Split(value, ",") {
				if tool = strings.TrimSpace(tool); tool != "" {
					r.Tools = append(r.Tools, tool)
				}
			}
		}
	}
	if r.Name == "" {
		return Role{}, fmt.Errorf("missing name")
	}
	r.Prompt = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return r, nil
}
