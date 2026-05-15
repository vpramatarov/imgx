// Package naming turns arbitrary user-supplied names into filesystem-safe
// ASCII stems and resolves output-path conflicts in a concurrency-safe way.
//
// Pipeline: Translit (Cyrillic via BGN/PCGN table, other scripts via
// go-unidecode) -> Sanitize (lowercase, [a-z0-9.-] only) -> Resolver.
package naming

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/mozillazg/go-unidecode"
)

// BGN/PCGN romanisation of Cyrillic (Russian, with common additions for
// Bulgarian / Ukrainian / Belarusian letters). Soft and hard signs are
// dropped rather than emitted as apostrophes, since filenames should not
// carry quotes.
var cyrillicBGNPCGN = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d",
	'е': "e", 'ё': "yo", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n",
	'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch",
	'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "",
	'э': "e", 'ю': "yu", 'я': "ya",
	// Extras used in Bulgarian / Ukrainian / Belarusian / Serbian.
	'і': "i", 'ї': "yi", 'є': "ye", 'ґ': "g", 'ў': "u",
	'ђ': "dj", 'ј': "j", 'љ': "lj", 'њ': "nj", 'ћ': "c", 'џ': "dz",
}

// Translit maps runes through the BGN/PCGN Cyrillic table and falls back
// to go-unidecode for anything else non-ASCII. Case of the source rune is
// preserved where meaningful (capital Cyrillic maps to a Title-cased Latin
// digraph, e.g. Ж -> Zh).
func Translit(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
		lower := unicode.ToLower(r)
		if mapped, ok := cyrillicBGNPCGN[lower]; ok {
			if len(mapped) == 0 {
				continue
			}
			if unicode.IsUpper(r) {
				b.WriteString(strings.ToUpper(mapped[:1]))
				b.WriteString(mapped[1:])
			} else {
				b.WriteString(mapped)
			}
			continue
		}
		b.WriteString(unidecode.Unidecode(string(r)))
	}
	return b.String()
}

// Sanitize produces a filesystem-safe lowercase name: characters outside
// [a-z0-9.-] are replaced with '-', consecutive separators are collapsed,
// and leading/trailing '-' and '.' are trimmed. Empty results fall back
// to "image".
func Sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSep := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
			prevSep = false
			continue
		}
		if !prevSep {
			b.WriteByte('-')
			prevSep = true
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "image"
	}
	return out
}

// Clean runs Translit then Sanitize.
func Clean(s string) string {
	return Sanitize(Translit(s))
}

// Resolver assigns unique output filenames. Safe for concurrent use by
// multiple pipeline workers.
type Resolver struct {
	mu       sync.Mutex
	dir      string
	reserved map[string]struct{}
}

// NewResolver creates a resolver bound to outDir.
func NewResolver(outDir string) *Resolver {
	return &Resolver{
		dir:      outDir,
		reserved: make(map[string]struct{}),
	}
}

// Resolve returns a free path of the form <dir>/<base>-<n>.<ext>, falling
// back to <base>-<n>-1.<ext>, <base>-<n>-2.<ext>, ... when there is a
// conflict with an existing file on disk or a previous Resolve call.
// The returned path is reserved; the caller is expected to write it.
func (r *Resolver) Resolve(base string, n int, ext string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stem := fmt.Sprintf("%s-%d", base, n)
	if p, ok, err := r.tryReserve(stem + "." + ext); err != nil {
		return "", err
	} else if ok {
		return p, nil
	}
	for i := 1; i < 1_000_000; i++ {
		name := fmt.Sprintf("%s-%d.%s", stem, i, ext)
		if p, ok, err := r.tryReserve(name); err != nil {
			return "", err
		} else if ok {
			return p, nil
		}
	}
	return "", errors.New("naming: could not find a free filename after 1M attempts")
}

func (r *Resolver) tryReserve(name string) (string, bool, error) {
	full := filepath.Join(r.dir, name)
	if _, taken := r.reserved[full]; taken {
		return "", false, nil
	}
	_, err := os.Stat(full)
	if err == nil {
		return "", false, nil
	}
	if !os.IsNotExist(err) {
		return "", false, err
	}
	r.reserved[full] = struct{}{}
	return full, true, nil
}
