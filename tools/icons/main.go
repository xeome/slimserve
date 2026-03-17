package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	config, err := loadConfig("config.json")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log.Printf("Fetching Heroicons v%s (%s style)...", config.Version, config.Style)

	icons, err := fetchIcons(config)
	if err != nil {
		return fmt.Errorf("failed to fetch icons: %w", err)
	}

	log.Printf("Fetched %d icons", len(icons))

	spritePath := filepath.Join("..", "..", "web", "static", "icons", "sprite.svg")
	if err := os.MkdirAll(filepath.Dir(spritePath), 0755); err != nil {
		return fmt.Errorf("failed to create icons directory: %w", err)
	}

	if err := generateSprite(icons, config.Version, spritePath); err != nil {
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
	client := &http.Client{Timeout: 10 * time.Second}
	var icons []Icon

	for _, name := range config.Icons {
		svg, err := fetchIcon(client, config, name)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s: %w", name, err)
		}
		icons = append(icons, Icon{Name: name, SVG: svg})
	}

	return icons, nil
}

func fetchIcon(client *http.Client, config *Config, name string) (string, error) {
	url := fmt.Sprintf("%s/24/%s/%s.svg", config.BaseURL, config.Style, name)

	resp, err := client.Get(url)
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

	// Replace hardcoded stroke/fill with currentColor for theme support
	svg := string(data)
	svg = strings.ReplaceAll(svg, `stroke="#0F172A"`, `stroke="currentColor"`)
	svg = strings.ReplaceAll(svg, `fill="#0F172A"`, `fill="currentColor"`)

	return svg, nil
}

func generateSprite(icons []Icon, version, outputPath string) error {
	var builder strings.Builder

	builder.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" style="display: none;">`)
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("<!-- Heroicons v%s - Outline Style -->\n", version))
	builder.WriteString(fmt.Sprintf("<!-- Generated on %s -->\n", time.Now().Format(time.RFC3339)))
	builder.WriteString("\n")

	for _, icon := range icons {
		svgContent := extractSVGContent(icon.SVG)

		builder.WriteString(fmt.Sprintf("  <symbol id=\"%s\" viewBox=\"0 0 24 24\">\n", icon.Name))
		builder.WriteString(fmt.Sprintf("    %s\n", svgContent))
		builder.WriteString("  </symbol>\n")
	}

	builder.WriteString("</svg>\n")

	return os.WriteFile(outputPath, []byte(builder.String()), 0644)
}

func extractSVGContent(svg string) string {
	// Simple approach: strip the outer <svg>...</svg> tags
	svg = strings.TrimSpace(svg)

	// Find the first > after <svg
	startIdx := strings.Index(svg, ">")
	if startIdx == -1 {
		return svg
	}
	startIdx++ // Move past the >

	// Find the closing </svg>
	endIdx := strings.LastIndex(svg, "</svg>")
	if endIdx == -1 {
		return svg
	}

	return strings.TrimSpace(svg[startIdx:endIdx])
}
