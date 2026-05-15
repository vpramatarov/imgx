// Package config resolves the effective Config from (in order):
//
//  1. Config file (YAML/TOML) at --config or ./imgx.{yaml,toml}
//  2. Environment variables prefixed IMGX_
//  3. Command-line flags (override everything)
//
// It also validates the result.
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Config struct {
	Input       string
	Output      string
	Name        string
	Width       int
	Height      int
	Fit         [2]int
	Format      string
	Quality     int
	Sort        string
	Recursive   bool
	ConfigPath  string
	DryRun      bool
	Verbose     bool
	Concurrency int
}

// Load reads the config sources, applies flag overrides, validates, and
// returns the merged Config. args contains positional arguments; the
// first element, if any, is treated as the input directory and wins
// over the `input:` field from a config file.
func Load(cmd *cobra.Command, args []string) (Config, error) {
	v := viper.New()
	v.SetEnvPrefix("IMGX")
	v.AutomaticEnv()
	v.SetDefault("quality", 85)
	v.SetDefault("concurrency", runtime.NumCPU())

	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return Config{}, fmt.Errorf("bind flags: %w", err)
	}

	if cfgPath, _ := cmd.Flags().GetString("config"); cfgPath != "" {
		v.SetConfigFile(cfgPath)
	} else {
		v.SetConfigName("imgx")
		v.AddConfigPath(".")
		v.AddConfigPath("./configs")
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	input := v.GetString("input")
	if len(args) > 0 {
		input = args[0]
	}

	cfg := Config{
		Input:       input,
		Output:      v.GetString("out"),
		Name:        v.GetString("name"),
		Width:       v.GetInt("width"),
		Height:      v.GetInt("height"),
		Format:      strings.ToLower(v.GetString("format")),
		Quality:     v.GetInt("quality"),
		Sort:        v.GetString("sort"),
		Recursive:   v.GetBool("recursive"),
		ConfigPath:  v.GetString("config"),
		DryRun:      v.GetBool("dry-run"),
		Verbose:     v.GetBool("verbose"),
		Concurrency: v.GetInt("concurrency"),
	}
	if fitStr := v.GetString("fit"); fitStr != "" {
		w, h, err := ParseFit(fitStr)
		if err != nil {
			return Config{}, err
		}
		cfg.Fit = [2]int{w, h}
	}
	if err := Validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ParseFit parses a "WxH" string (e.g. "1200x1200") into positive width
// and height. Returned error messages match the --fit flag phrasing.
func ParseFit(s string) (int, int, error) {
	parts := strings.SplitN(strings.ToLower(s), "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("--fit must be WxH, got %q", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("--fit width invalid: %q", parts[0])
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("--fit height invalid: %q", parts[1])
	}
	return w, h, nil
}

// Validate normalises defaults and returns an error on invalid input.
func Validate(c *Config) error {
	if c.Input == "" {
		return errors.New("input directory is required (pass as positional arg or `input:` in config)")
	}
	if c.Output == "" {
		return errors.New("--out is required")
	}
	resizeModes := 0
	if c.Width > 0 {
		resizeModes++
	}
	if c.Height > 0 {
		resizeModes++
	}
	if c.Fit[0] > 0 || c.Fit[1] > 0 {
		resizeModes++
	}
	if resizeModes > 1 {
		return errors.New("--width, --height and --fit are mutually exclusive")
	}
	if c.Quality < 1 || c.Quality > 100 {
		return fmt.Errorf("--quality must be 1..100, got %d", c.Quality)
	}
	if c.Concurrency < 1 {
		c.Concurrency = 1
	}
	if c.Name == "" {
		c.Name = filepath.Base(c.Input)
	}
	return nil
}
