package storage

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONMap 对应一个保存 JSON 对象的 jsonb 列。
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = JSONMap{}
		return nil
	}
	b, err := asBytes(src)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		*m = JSONMap{}
		return nil
	}
	return json.Unmarshal(b, m)
}

// GormDataType 告诉 GORM 这些值应按 jsonb 参数读写。
func (JSONMap) GormDataType() string { return "jsonb" }

// JSONDoc 对应一个保存任意 JSON（对象或数组）的 jsonb 列。
type JSONDoc json.RawMessage

func (d JSONDoc) Value() (driver.Value, error) {
	if len(d) == 0 {
		return []byte("null"), nil
	}
	return []byte(d), nil
}

func (d *JSONDoc) Scan(src any) error {
	if src == nil {
		*d = nil
		return nil
	}
	b, err := asBytes(src)
	if err != nil {
		return err
	}
	out := make([]byte, len(b))
	copy(out, b)
	*d = out
	return nil
}

func (JSONDoc) GormDataType() string { return "jsonb" }

// MarshalJSON 让 JSONDoc 嵌入 API 响应时保持透明。
func (d JSONDoc) MarshalJSON() ([]byte, error) {
	if len(d) == 0 {
		return []byte("null"), nil
	}
	return d, nil
}

func (d *JSONDoc) UnmarshalJSON(b []byte) error {
	out := make([]byte, len(b))
	copy(out, b)
	*d = out
	return nil
}

func asBytes(src any) ([]byte, error) {
	switch v := src.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("jsonb 不支持的扫描类型 %T", src)
	}
}
