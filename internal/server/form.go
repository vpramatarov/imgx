package server

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/vpramatarov/imgx/internal/config"
)

// formInputs captures the raw form selections. Callers turn the paired
// *multipart.FileHeader list into files on disk, then call toConfig to
// produce a validated config.Config pointing at the populated temp dirs.
type formInputs struct {
	Name       string
	ResizeMode string
	Width      int
	Height     int
	Fit        [2]int
	Format     string
	Quality    int
	Sort       string
	Files      []*multipart.FileHeader
}

// parseInputs reads form fields (the request must already have had
// ParseMultipartForm called on it) and returns a formInputs. It does not
// touch the filesystem; disk work is the caller's job.
func parseInputs(r *http.Request) (formInputs, error) {
	in := formInputs{
		Name:       strings.TrimSpace(r.FormValue("name")),
		ResizeMode: r.FormValue("resize-mode"),
		Format:     strings.ToLower(strings.TrimSpace(r.FormValue("format"))),
		Sort:       r.FormValue("sort"),
	}
	if r.MultipartForm != nil {
		in.Files = r.MultipartForm.File["files"]
	}
	if len(in.Files) == 0 {
		return in, errors.New("no files uploaded (field name: files)")
	}

	switch in.ResizeMode {
	case "", "none":
		// no-op
	case "width":
		w, err := parsePositive(r.FormValue("width"), "width")
		if err != nil {
			return in, err
		}
		in.Width = w
	case "height":
		h, err := parsePositive(r.FormValue("height"), "height")
		if err != nil {
			return in, err
		}
		in.Height = h
	case "fit":
		w, h, err := config.ParseFit(r.FormValue("fit"))
		if err != nil {
			return in, err
		}
		in.Fit = [2]int{w, h}
	default:
		return in, fmt.Errorf("invalid resize-mode %q", in.ResizeMode)
	}

	if q := strings.TrimSpace(r.FormValue("quality")); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil {
			return in, fmt.Errorf("invalid quality %q", q)
		}
		in.Quality = n
	}
	return in, nil
}

// toConfig builds a validated Config from parsed inputs + resolved
// input/output directories. Defaults that Validate won't fill are
// applied here (Name fallback, Quality default).
func (in formInputs) toConfig(inputDir, outputDir string, concurrency int) (config.Config, error) {
	cfg := config.Config{
		Input:       inputDir,
		Output:      outputDir,
		Name:        in.Name,
		Width:       in.Width,
		Height:      in.Height,
		Fit:         in.Fit,
		Format:      in.Format,
		Quality:     in.Quality,
		Sort:        in.Sort,
		Concurrency: concurrency,
	}
	if cfg.Name == "" {
		// Validate would otherwise pin this to filepath.Base(inputDir),
		// which is the random mkdtemp suffix — ugly in filenames.
		cfg.Name = "image"
	}
	if cfg.Quality == 0 {
		cfg.Quality = 85
	}
	if err := config.Validate(&cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func parsePositive(s, field string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%s is required for this resize mode", field)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", field, s)
	}
	return n, nil
}
