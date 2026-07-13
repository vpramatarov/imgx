package naming

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTranslitCyrillic(t *testing.T) {
	cases := map[string]string{
		"Пловдив":     "Plovdiv",
		"Щука":        "Shchuka",
		"Йошкар-Ола":  "Yoshkar-Ola",
		"Жёлтый":      "Zhyoltyy",
		"София":       "Sofiya",
		"Ёлка":        "Yolka",
		"велизар":     "velizar",
	}
	for in, want := range cases {
		got := Translit(in)
		if got != want {
			t.Errorf("Translit(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTranslitFallbackUnidecode(t *testing.T) {
	// Greek "α" → "a" via unidecode fallback.
	if got := Translit("αβγ"); got == "αβγ" {
		t.Errorf("Translit(Greek) was not transliterated: %q", got)
	}
	// ASCII is passed through untouched.
	if got := Translit("hello_world-1"); got != "hello_world-1" {
		t.Errorf("ASCII should pass through, got %q", got)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"Hello World":    "hello-world",
		"  spaces  ":     "spaces",
		"a//b\\c":        "a-b-c",
		"---leading":     "leading",
		"trail---":       "trail",
		"UPPER":          "upper",
		"a_b_c":          "a-b-c",
		"file.ext":       "file.ext",
		"":               "image",
		"###":            "image",
		"über_cool":      "ber-cool", // Sanitize drops non-ASCII; Translit would have run first in Clean.
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClean(t *testing.T) {
	cases := map[string]string{
		"Пловдив фото":    "plovdiv-foto",
		"Йошкар-Ола 2024": "yoshkar-ola-2024",
		"über cool":       "uber-cool",
		"  my IMG ":       "my-img",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolverNoConflict(t *testing.T) {
	dir := t.TempDir()
	r := NewResolver(dir)
	got, err := r.Resolve("img", 1, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "img-1.jpg")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolverConflictOnDisk(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "img-1.jpg")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(dir)
	got, err := r.Resolve("img", 1, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "img-1-1.jpg")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveStemNoConflict(t *testing.T) {
	dir := t.TempDir()
	r := NewResolver(dir)
	got, err := r.ResolveStem("holiday-photo", "jpg")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "holiday-photo.jpg")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveStemConflicts(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(dir)
	for i, base := range []string{"photo-1.jpg", "photo-2.jpg"} {
		got, err := r.ResolveStem("photo", "jpg")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, base)
		if got != want {
			t.Errorf("call %d: got %q, want %q", i+1, got, want)
		}
	}
}

func TestResolverConcurrentReservations(t *testing.T) {
	dir := t.TempDir()
	r := NewResolver(dir)
	const n = 50
	results := make(chan string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			p, err := r.Resolve("img", 1, "jpg")
			if err != nil {
				t.Errorf("resolve: %v", err)
				return
			}
			results <- p
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for p := range results {
		if seen[p] {
			t.Errorf("resolver handed out duplicate path %q", p)
		}
		seen[p] = true
	}
	if len(seen) != n {
		t.Errorf("expected %d unique paths, got %d", n, len(seen))
	}
}
