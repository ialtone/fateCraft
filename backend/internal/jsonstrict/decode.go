// Package jsonstrict supplies bounded, unambiguous decoding for authored content.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

func Decode(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("JSON 必须使用有效 UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := value(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("只能包含一个 JSON 值")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}

func value(d *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON 嵌套超过 64 层")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return fmt.Errorf("重复或无效 JSON 字段 %v", key)
				}
				seen[k] = true
				if err := value(d, depth+1); err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
			}
		case '[':
			for d.More() {
				if err := value(d, depth+1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("无效 JSON 分隔符")
		}
		_, err = d.Token()
		return err
	case json.Number:
		if len(v) > 128 {
			return fmt.Errorf("JSON 数值文本超过 128 字节")
		}
		if position := strings.IndexAny(string(v), "eE"); position >= 0 {
			exponent, err := strconv.ParseInt(string(v)[position+1:], 10, 32)
			if err != nil || exponent < -324 || exponent > 324 {
				return fmt.Errorf("JSON 数值指数超限")
			}
		}
		n, err := strconv.ParseFloat(string(v), 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || math.Abs(n) > 9007199254740991 {
			return fmt.Errorf("数值超出平台精确范围: %s", v)
		}
		original, ok := new(big.Rat).SetString(string(v))
		encoded, _ := json.Marshal(n)
		converted, safe := new(big.Rat).SetString(string(encoded))
		if !ok || !safe || original.Cmp(converted) != 0 {
			return fmt.Errorf("数值不能无损表达: %s", v)
		}
	}
	return nil
}
