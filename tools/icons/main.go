package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Version string   `json:"version"`
	Style   string   `json:"style"`
	BaseURL string   `json:"baseUrl"`
	Icons   []string `json:"icons"`
}

type Icon struct {
	Name string
	SVG  string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Load config
	config, err := loadConfig("config.json")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log.Printf("Fetching Heroicons v%s (%s style)...", config.Version, config.Style)

	// Fetch all icons concurrently
	icons, err := fetchIcons(config)
	if err != nil {
		return fmt.Errorf("failed to fetch icons: %w", err)
	}

	log.Printf("Fetched %d icons", len(icons))

	// Generate sprite.svg
	spritePath := filepath.Join("..", "..", "web", "static", "icons", "sprite.svg")
	if err := os.MkdirAll(filepath.Dir(spritePath), 0755); err != nil {
		return fmt.Errorf("failed to create icons directory: %w", err)
	}

	if err := generateSprite(icons, spritePath); err != nil {
		return fmt.Errorf("failed to generate sprite: %w", err)
	}

	log.Printf("Generated sprite.svg at %s", spritePath)

	return nil
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func fetchIcons(config *Config) ([]Icon, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	iconChan := make(chan Icon, len(config.Icons))
	errChan := make(chan error, len(config.Icons))

	// Semaphore to limit concurrent requests
	semaphore := make(chan struct{}, 5)

	for _, name := range config.Icons {
		wg.Add(1)
		go func(iconName string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			svg, err := fetchIcon(ctx, client, config, iconName)
			if err != nil {
				errChan <- fmt.Errorf("failed to fetch %s: %w", iconName, err)
				return
			}

			iconChan <- Icon{Name: iconName, SVG: svg}
		}(name)
	}

	// Close channels when done
	go func() {
		wg.Wait()
		close(iconChan)
		close(errChan)
	}()

	// Collect results
	var icons []Icon
	var errs []error

	for icon := range iconChan {
		icons = append(icons, icon)
	}

	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("encountered %d errors during fetch", len(errs))
	}

	return icons, nil
}

func fetchIcon(ctx context.Context, client *http.Client, config *Config, name string) (string, error) {
	// Map legacy icon names to Heroicons v2 names
	name = normalizeIconName(name)

	url := fmt.Sprintf("%s/24/%s/%s.svg", config.BaseURL, config.Style, name)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Replace hardcoded stroke color with currentColor for theme support
	svg := string(data)
	svg = strings.ReplaceAll(svg, `stroke="#0F172A"`, `stroke="currentColor"`)
	svg = strings.ReplaceAll(svg, `fill="#0F172A"`, `fill="currentColor"`)

	return svg, nil
}

func normalizeIconName(name string) string {
	// Map some icon names to their v2 equivalents
	mappings := map[string]string{
		"photo":                    "photo",
		"photograph":               "photo",
		"musical-note":             "musical-note",
		"music-note":               "musical-note",
		"archive-box":              "archive-box",
		"archive":                  "archive-box",
		"cpu-chip":                 "cpu-chip",
		"memory":                   "cpu-chip",
		"circle-stack":             "circle-stack",
		"database":                 "circle-stack",
		"cloud-arrow-up":           "cloud-arrow-up",
		"cloud-upload":             "cloud-arrow-up",
		"eye-slash":                "eye-slash",
		"eye-off":                  "eye-slash",
		"cog-6-tooth":              "cog-6-tooth",
		"cog":                      "cog-6-tooth",
		"settings":                 "cog-6-tooth",
		"x-mark":                   "x-mark",
		"x":                        "x-mark",
		"check":                    "check",
		"arrow-up-tray":            "arrow-up-tray",
		"upload":                   "arrow-up-tray",
		"chart-bar":                "chart-bar",
		"chart-bar-square":         "chart-bar-square",
		"bolt":                     "bolt",
		"lightning-bolt":           "bolt",
		"chevron-right":            "chevron-right",
		"squares-2x2":              "squares-2x2",
		"grid":                     "squares-2x2",
		"bars-3":                   "bars-3",
		"list":                     "bars-3",
		"sun":                      "sun",
		"moon":                     "moon",
		"x-circle":                 "x-circle",
		"home":                     "home",
		"folder-open":              "folder-open",
		"lock-closed":              "lock-closed",
		"user":                     "user",
		"arrow-right-on-rectangle": "arrow-right-on-rectangle",
		"login":                    "arrow-right-on-rectangle",
		"arrow-left-on-rectangle":  "arrow-left-on-rectangle",
		"logout":                   "arrow-left-on-rectangle",
		"magnifying-glass":         "magnifying-glass",
		"search":                   "magnifying-glass",
		"arrow-down-tray":          "arrow-down-tray",
		"download":                 "arrow-down-tray",
		"pencil-square":            "pencil-square",
		"edit":                     "pencil-square",
		"server":                   "server",
		"folder":                   "folder",
		"document":                 "document",
		"document-text":            "document-text",
		"file":                     "document",
		"check-circle":             "check-circle",
		"clock":                    "clock",
		"eye":                      "eye",
		"trash":                    "trash",
		"plus":                     "plus",
		"information-circle":       "information-circle",
	}

	if mapped, ok := mappings[name]; ok {
		return mapped
	}
	return name
}

func generateSprite(icons []Icon, outputPath string) error {
	var builder strings.Builder

	builder.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" style="display: none;">`)
	builder.WriteString("\n")
	builder.WriteString("<!-- Heroicons v2.2.0 - Outline Style -->\n")
	builder.WriteString(fmt.Sprintf("<!-- Generated on %s -->\n", time.Now().Format(time.RFC3339)))
	builder.WriteString("\n")

	for _, icon := range icons {
		symbolID := normalizeIconName(icon.Name)
		svgContent := extractSVGContent(icon.SVG)

		builder.WriteString(fmt.Sprintf("  <symbol id=\"%s\" viewBox=\"0 0 24 24\">\n", symbolID))
		builder.WriteString(fmt.Sprintf("    %s\n", svgContent))
		builder.WriteString("  </symbol>\n")
	}

	builder.WriteString("</svg>\n")

	return os.WriteFile(outputPath, []byte(builder.String()), 0644)
}

func extractSVGContent(svg string) string {
	// Extract just the inner content (paths, etc.) from the SVG
	// Remove <svg> tags and xmlns attributes
	svg = strings.TrimSpace(svg)

	// Find content between <svg ...> and </svg>
	startIdx := strings.Index(svg, ">")
	if startIdx == -1 {
		return svg
	}
	startIdx++ // Move past the >

	endIdx := strings.LastIndex(svg, "</svg>")
	if endIdx == -1 {
		return svg
	}

	content := svg[startIdx:endIdx]
	content = strings.TrimSpace(content)

	return content
}
