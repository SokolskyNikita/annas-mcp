package filetransfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SanitizeFilename preserves readable Unicode while avoiding reserved names,
// path traversal and platform filename limits.
func SanitizeFilename(filename string) string {
	safe := unsafeFilenameChars.ReplaceAllString(filename, "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	safe = strings.TrimRight(filepath.Base(safe), " .")
	if safe == "" {
		return ""
	}
	stem := strings.ToUpper(strings.SplitN(safe, ".", 2)[0])
	stem = strings.TrimRight(stem, " .")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		safe = "_" + safe
	}
	const maxFilenameBytes = 180
	if len(safe) <= maxFilenameBytes {
		return safe
	}
	var result strings.Builder
	for _, r := range safe {
		if result.Len()+utf8.RuneLen(r) > maxFilenameBytes {
			break
		}
		result.WriteRune(r)
	}
	return strings.TrimRight(result.String(), " .")
}

func finalize(ctx context.Context, temp, directory, title, extension, key string) (string, error) {
	base := SanitizeFilename(title)
	if base == "" {
		base = "untitled"
	}
	digest := sha256.Sum256([]byte(key))
	suffix := hex.EncodeToString(digest[:4])
	for index := 0; ; index++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		name := base + "." + extension
		if index == 1 {
			name = base + "-" + suffix + "." + extension
		}
		if index > 1 {
			name = fmt.Sprintf("%s-%s-%d.%s", base, suffix, index, extension)
		}
		filename := filepath.Join(directory, name)
		if err := os.Link(temp, filename); err == nil {
			return filename, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", localError(err)
		}
	}
}
