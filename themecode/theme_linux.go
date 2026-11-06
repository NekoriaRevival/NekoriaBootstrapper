//go:build linux

package themecode

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

func DetectSystemTheme() fyne.ThemeVariant {
	desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	if strings.Contains(desktop, "kde") || strings.Contains(desktop, "plasma") {
		return detectKDETheme()
	}
	return detectGnomeTheme()
}
func detectKDETheme() fyne.ThemeVariant {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Println("KDE: Could not find user home directory:", err)
		return theme.VariantDark
	}
	configFile := filepath.Join(home, ".config", "kdeglobals")
	file, err := os.Open(configFile)
	if err != nil {
		log.Println("KDE: Could not open config file:", err)
		return theme.VariantDark
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	inGeneralSection := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "[General]" {
			inGeneralSection = true
			continue
		}
		if inGeneralSection && strings.HasPrefix(line, "[") {
			break
		}
		if inGeneralSection && strings.HasPrefix(line, "ColorScheme=") {
			value := strings.SplitN(line, "=", 2)[1]
			if strings.Contains(strings.ToLower(value), "dark") {
				return theme.VariantDark
			}
			return theme.VariantLight
		}
	}
	log.Println("KDE: Could not find ColorScheme in kdeglobals, falling back to light.")
	return theme.VariantDark
}
func detectGnomeTheme() fyne.ThemeVariant {
	cmd := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
	output, err := cmd.Output()
	if err != nil {
		log.Println("GNOME: Could not run gsettings, falling back to light theme:", err)
		return theme.VariantDark
	}
	result := strings.TrimSpace(string(output))
	result = strings.Trim(result, "'")
	if result == "prefer-dark" {
		return theme.VariantDark
	}
	return theme.VariantLight
}
