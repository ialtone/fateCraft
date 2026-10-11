package challengeimport

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type bootstrapManifest struct {
	Version int      `json:"version"`
	Packs   []string `json:"packs"`
}

func bootstrapDirectories(dataDir string) ([]string, error) {
	file, err := os.Open(filepath.Join(dataDir, "bootstrap.json"))
	if err != nil {
		return nil, fmt.Errorf("读取默认玩法清单: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024+1))
	decoder.DisallowUnknownFields()
	var manifest bootstrapManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("默认玩法清单无效: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("默认玩法清单包含多余内容")
	}
	if manifest.Version != 1 || manifest.Packs == nil || len(manifest.Packs) > 64 {
		return nil, fmt.Errorf("默认玩法清单版本或数量无效")
	}
	seen := map[string]bool{}
	directories := make([]string, 0, len(manifest.Packs))
	for _, name := range manifest.Packs {
		if name == "" || name != strings.TrimSpace(name) || name == "." || name == ".." || strings.ContainsAny(name, "/\\:") || seen[name] {
			return nil, fmt.Errorf("默认玩法目录无效或重复: %q", name)
		}
		seen[name] = true
		directory := filepath.Join(dataDir, "preset_packs", name)
		info, err := os.Lstat(directory)
		if err != nil {
			return nil, fmt.Errorf("默认玩法 %s 不存在: %w", name, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("默认玩法 %s 必须是普通目录", name)
		}
		directories = append(directories, directory)
	}
	return directories, nil
}
