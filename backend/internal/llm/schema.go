package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func SchemaFingerprint(schema *JSONSchema) string {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
