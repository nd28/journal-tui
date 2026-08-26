package cli

import (
	"os"
	"strconv"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
