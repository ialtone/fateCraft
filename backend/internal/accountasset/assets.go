// Package accountasset defines platform account units independently of storage.
package accountasset

const (
	Points               = "fatecraft.points"
	RewriteCredits       = "fatecraft.message-rewrite-credit"
	MaxQuantity    int64 = 9007199254740991
)

func ValidKey(key string) bool { return key == Points || key == RewriteCredits }
