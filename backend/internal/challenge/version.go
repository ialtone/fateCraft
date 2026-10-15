package challenge

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseStableVersion(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("版本必须使用 major.minor.patch")
	}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return out, fmt.Errorf("稳定版本号无效")
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return out, fmt.Errorf("稳定版本号无效")
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, err
		}
		out[i] = n
	}
	return out, nil
}
func CompareStableVersions(a, b string) (int, error) {
	left, err := ParseStableVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := ParseStableVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range left {
		if left[i] < right[i] {
			return -1, nil
		}
		if left[i] > right[i] {
			return 1, nil
		}
	}
	return 0, nil
}
