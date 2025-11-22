package vars

import "strings"

func StrToBool(str string) bool {
	str = strings.ToLower(str)
	switch str {
	case "true", "t", "yes", "y":
		return true
	case "false", "f", "no", "n":
		return false
	}
	return false
}
