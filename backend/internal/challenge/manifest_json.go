package challenge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func strictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("JSON 含有多余内容")
	}
	return nil
}

func (m *Manifest) UnmarshalJSON(raw []byte) error {
	type plain Manifest
	return strictJSON(raw, (*plain)(m))
}

func (m *SaveVersionInput) UnmarshalJSON(raw []byte) error {
	type plain SaveVersionInput
	return strictJSON(raw, (*plain)(m))
}
