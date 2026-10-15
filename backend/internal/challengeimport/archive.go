package challengeimport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ialtone/fatecraft/internal/challenge"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/jsonstrict"
)

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type assetIndex struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}
type exchangeMetadata struct {
	RuntimeEntry           string       `json:"runtime_entry,omitempty"`
	Format                 string       `json:"format"`
	FormatVersion          int          `json:"format_version"`
	RuntimeContractVersion int          `json:"runtime_contract_version"`
	ContentHash            string       `json:"content_hash,omitempty"`
	Provenance             *Provenance  `json:"provenance,omitempty"`
	Assets                 []assetIndex `json:"assets"`
}

func safeArchivePath(name string) bool {
	if name == "" || len(name) > 240 || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") {
			return false
		}
		switch strings.ToUpper(strings.SplitN(part, ".", 2)[0]) {
		case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return false
		}
	}
	return true
}

func decodeArchive(raw []byte, registry *gameplay.Registry) (*loadedChallenge, *PortablePackage, error) {
	if len(raw) > MaxUploadBytes {
		return nil, nil, fmt.Errorf("上传文件超过 16 MiB")
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, nil, fmt.Errorf("无效 ZIP: %w", err)
	}
	if len(z.File) > 512 {
		return nil, nil, fmt.Errorf("ZIP 文件数超过 512")
	}
	files := map[string][]byte{}
	seen := map[string]bool{}
	total := int64(0)
	root := ""
	roots := 0
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !safeArchivePath(name) || seen[strings.ToLower(name)] || f.Mode()&os.ModeSymlink != 0 || (!f.Mode().IsRegular() && !f.FileInfo().IsDir()) || f.Flags&1 != 0 {
			return nil, nil, packageError("package_path_invalid", "ZIP 路径、条目类型或重复项无效: %s", f.Name)
		}
		seen[strings.ToLower(name)] = true
		if f.FileInfo().IsDir() {
			continue
		}
		base := strings.ToLower(path.Base(name))
		ext := strings.ToLower(path.Ext(name))
		if ext != ".json" && base != "readme" && base != "readme.md" && base != "license" && base != "license.txt" {
			return nil, nil, fmt.Errorf("ZIP 含不支持文件: %s", name)
		}
		if f.UncompressedSize64 > 2<<20 || (f.UncompressedSize64 > 0 && (f.CompressedSize64 == 0 || f.UncompressedSize64 > f.CompressedSize64*100)) {
			return nil, nil, packageError("package_too_large", "ZIP 条目大小或解压比超限: %s", name)
		}
		total += int64(f.UncompressedSize64)
		if total > 32<<20 {
			return nil, nil, packageError("package_too_large", "ZIP 解压总量超过 32 MiB")
		}
		reader, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, (2<<20)+1))
		reader.Close()
		if readErr != nil || len(data) > 2<<20 {
			return nil, nil, fmt.Errorf("ZIP 条目读取失败或超限: %s", name)
		}
		files[name] = data
		if path.Base(name) == "pack.json" {
			root = path.Dir(name)
			roots++
		}
	}
	if roots != 1 || (root != "." && strings.Contains(root, "/")) {
		return nil, nil, fmt.Errorf("ZIP 必须包含唯一根玩法包，可有一层外目录")
	}
	dir, err := os.MkdirTemp("", "fatecraft-package-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	for name, data := range files {
		relative := name
		if root != "." {
			if !strings.HasPrefix(name, root+"/") {
				return nil, nil, fmt.Errorf("ZIP 含包外文件")
			}
			relative = strings.TrimPrefix(name, root+"/")
		}
		if !allowedSourceFile(relative) {
			return nil, nil, fmt.Errorf("ZIP 含未知运行文件: %s", relative)
		}
		target := filepath.Join(dir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			return nil, nil, err
		}
	}
	loaded, err := loadDirectory(dir, time.Now(), registry)
	if err != nil {
		return nil, nil, fmt.Errorf("%s", strings.ReplaceAll(err.Error(), dir, "<package>"))
	}
	p, err := portableFromDocument(&loaded.document)
	if err != nil {
		return nil, nil, err
	}
	var meta exchangeMetadata
	if data, ok := files[path.Join(root, "fatecraft-package.json")]; ok {
		if err := jsonstrict.Decode(data, &meta); err != nil {
			return nil, nil, err
		}
		p.Provenance = meta.Provenance
	}
	p.ContentHash = meta.ContentHash
	return loaded, p, nil
}

func allowedSourceFile(name string) bool {
	if name == "pack.json" || name == "fatecraft-package.json" || name == "definitions.json" {
		return true
	}
	base := strings.ToLower(path.Base(name))
	if !strings.Contains(name, "/") && (base == "readme" || base == "readme.md" || base == "license" || base == "license.txt") {
		return true
	}
	parts := strings.Split(name, "/")
	if len(parts) < 2 || path.Ext(name) != ".json" {
		return false
	}
	if parts[0] == "runtime" {
		return true
	}
	if len(parts) != 2 {
		return false
	}
	if parts[0] == "definitions" {
		return true
	}
	for _, s := range assetSources {
		if parts[0] == s.directory {
			return true
		}
	}
	return false
}

func encodeArchive(p *PortablePackage) ([]byte, error) {
	files := map[string]any{}
	manifest := p.Manifest
	manifest.Outcomes = p.Outcomes
	entryPath := manifest.RuntimeEntry
	if entryPath == "" {
		entryPath = "runtime/runtime.json"
	}
	if !safeArchivePath(entryPath) || !strings.HasPrefix(entryPath, "runtime/") || path.Ext(entryPath) != ".json" {
		return nil, fmt.Errorf("runtime_entry 不能安全映射为目录包")
	}
	files["pack.json"] = manifest
	entryDir := path.Dir(entryPath)
	prefix := "exchange-"
	if strings.HasPrefix(path.Base(entryPath), prefix) {
		prefix = "repack-"
	}
	includes := runtimeSourceIncludes{StateSchema: prefix + "state.json", Actions: prefix + "actions.json", Settlement: prefix + "settlement.json", Presentation: prefix + "presentation.json"}
	if p.Runtime.Campaign != nil {
		includes.Campaign = prefix + "campaign.json"
		files[path.Join(entryDir, includes.Campaign)] = runtimeCampaignFragment{p.Runtime.Campaign}
	}
	files[entryPath] = runtimeSourceEntry{Schema: "fatecraft.runtime.schema.json", ContractVersion: 2, Engine: p.Runtime.Engine, Mode: p.Runtime.Mode, Includes: includes}
	files[path.Join(entryDir, includes.StateSchema)] = runtimeStateSchemaFragment{&p.Runtime.StateSchema}
	files[path.Join(entryDir, includes.Actions)] = runtimeActionsFragment{&p.Runtime.Actions}
	files[path.Join(entryDir, includes.Settlement)] = runtimeSettlementFragment{&p.Runtime.Settlement}
	files[path.Join(entryDir, includes.Presentation)] = runtimePresentationFragment{&p.Runtime.Presentation}
	files["definitions.json"] = p.Definitions
	meta := exchangeMetadata{RuntimeEntry: entryPath, Format: PackageFormat, FormatVersion: 1, RuntimeContractVersion: 2, ContentHash: p.ContentHash, Provenance: p.Provenance, Assets: []assetIndex{}}
	for index, a := range p.Assets {
		directory := ""
		for _, s := range assetSources {
			if s.kind == a.Kind {
				directory = s.directory
			}
		}
		if directory == "" {
			return nil, fmt.Errorf("资产类型无效")
		}
		name := fmt.Sprintf("%s/%04d.json", directory, index)
		files[name] = a.Content
		meta.Assets = append(meta.Assets, assetIndex{name, a.Kind, a.Slug, a.Name, a.SortOrder})
	}
	files["fatecraft-package.json"] = meta
	if len(files) > 512 {
		return nil, fmt.Errorf("目录文件数超过 ZIP 上限，请使用 JSON")
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range sortedKeys(files) {
		raw, err := json.MarshalIndent(files[name], "", "  ")
		if err != nil {
			return nil, err
		}
		if len(raw) > 2<<20 {
			return nil, fmt.Errorf("导出目录分片超过 2 MiB，请使用 JSON")
		}
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		w, err := writer.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(raw); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > MaxUploadBytes {
		return nil, fmt.Errorf("导出 ZIP 超过交换上限")
	}
	return buffer.Bytes(), nil
}

func applyExchangeMetadata(directory string, doc *challenge.VersionDocument) (string, error) {
	raw, err := os.ReadFile(filepath.Join(directory, "fatecraft-package.json"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var m exchangeMetadata
	if err := jsonstrict.Decode(raw, &m); err != nil {
		return "", err
	}
	if m.Format != PackageFormat || m.FormatVersion != 1 || m.RuntimeContractVersion != 2 || len(m.Assets) != len(doc.Assets) {
		return "", fmt.Errorf("交换索引格式或资产数量无效")
	}
	seen := map[string]bool{}
	seenIdentity := map[string]bool{}
	for _, index := range m.Assets {
		if !safeArchivePath(index.Path) || seen[index.Path] {
			return "", fmt.Errorf("交换资产索引路径无效")
		}
		seen[index.Path] = true
		identity := index.Kind + ":" + index.Slug
		if seenIdentity[identity] {
			return "", fmt.Errorf("交换索引重复资产身份")
		}
		seenIdentity[identity] = true
		expectedDir := ""
		for _, s := range assetSources {
			if s.kind == index.Kind {
				expectedDir = s.directory
			}
		}
		if expectedDir == "" || path.Dir(index.Path) != expectedDir {
			return "", fmt.Errorf("交换索引资产目录与类型不一致")
		}
		var value map[string]any
		if err := decodeJSONFileStrict(filepath.Join(directory, filepath.FromSlash(index.Path)), &value); err != nil {
			return "", err
		}
		if value["id"] != index.Slug || value["name"] != index.Name {
			return "", fmt.Errorf("交换资产索引与正文不一致")
		}
		found := false
		for i := range doc.Assets {
			a := &doc.Assets[i]
			if a.Kind == index.Kind && a.Slug == index.Slug {
				if found {
					return "", fmt.Errorf("重复资产身份")
				}
				a.SortOrder = index.SortOrder
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("交换资产索引引用缺失")
		}
	}
	if m.Provenance != nil {
		raw, err := json.Marshal(m.Provenance)
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(raw, &doc.Version.Provenance); err != nil {
			return "", err
		}
	}
	return m.ContentHash, nil
}
