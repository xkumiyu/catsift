package githubcopilot

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxWorkspaceMetadataBytes = 1 << 20

type workspaceMetadata struct {
	Name       string
	Repository string
	GitRoot    string
	CWD        string
}

func workspaceMetadataPath(eventPath string) string {
	return filepath.Join(filepath.Dir(eventPath), "workspace.yaml")
}

func readWorkspaceName(path string) (name string, err error) {
	metadata, err := readWorkspaceMetadata(path)
	return metadata.Name, err
}

func readWorkspaceMetadata(path string) (metadata workspaceMetadata, err error) {
	file, err := os.Open(path)
	if err != nil {
		return workspaceMetadata{}, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	data, err := io.ReadAll(io.LimitReader(file, maxWorkspaceMetadataBytes+1))
	if err != nil {
		return workspaceMetadata{}, err
	}
	if len(data) > maxWorkspaceMetadataBytes {
		return workspaceMetadata{}, fmt.Errorf("workspace metadata exceeds %d bytes", maxWorkspaceMetadataBytes)
	}
	return parseWorkspaceMetadata(string(data))
}

func parseWorkspaceName(document string) (string, error) {
	metadata, err := parseWorkspaceMetadata(document)
	return metadata.Name, err
}

func parseWorkspaceMetadata(document string) (workspaceMetadata, error) {
	var metadata workspaceMetadata
	lines := strings.Split(strings.ReplaceAll(document, "\r\n", "\n"), "\n")
	for index, line := range lines {
		if index == 0 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "name" && key != "repository" && key != "git_root" && key != "cwd" {
			continue
		}
		value = strings.TrimSpace(stripYAMLComment(value))
		if value == "" || value == "~" || strings.EqualFold(value, "null") {
			continue
		}
		if value[0] == '|' || value[0] == '>' {
			value = parseWorkspaceBlock(lines[index+1:], value[0])
		} else {
			parsed, err := parseWorkspaceScalar(value)
			if err != nil {
				return workspaceMetadata{}, err
			}
			value = parsed
		}
		switch key {
		case "name":
			metadata.Name = value
		case "repository":
			metadata.Repository = value
		case "git_root":
			metadata.GitRoot = value
		case "cwd":
			metadata.CWD = value
		}
	}
	return metadata, nil
}

func parseWorkspaceScalar(value string) (string, error) {
	switch value[0] {
	case '"':
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("parse workspace name: %w", err)
		}
		return strings.TrimSpace(parsed), nil
	case '\'':
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", fmt.Errorf("parse workspace name: unterminated single quote")
		}
		return strings.TrimSpace(strings.ReplaceAll(value[1:len(value)-1], "''", "'")), nil
	default:
		return strings.TrimSpace(value), nil
	}
}

func parseWorkspaceBlock(lines []string, style byte) string {
	indent := -1
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			values = append(values, "")
			continue
		}
		currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
		if currentIndent == 0 {
			break
		}
		if indent < 0 {
			indent = currentIndent
		}
		if currentIndent < indent {
			break
		}
		values = append(values, line[minIndent(indent, len(line)):])
	}
	separator := "\n"
	if style == '>' {
		separator = " "
	}
	return strings.TrimSpace(strings.Join(values, separator))
}

func minIndent(value, limit int) int {
	if value > limit {
		return limit
	}
	return value
}

func stripYAMLComment(value string) string {
	quoted := byte(0)
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\'', '"':
			if quoted == 0 {
				quoted = value[index]
			} else if quoted == value[index] && (index == 0 || value[index-1] != '\\') {
				quoted = 0
			}
		case '#':
			if quoted == 0 && (index == 0 || value[index-1] == ' ' || value[index-1] == '\t') {
				return value[:index]
			}
		}
	}
	return value
}
