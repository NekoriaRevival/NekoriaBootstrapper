//go:build windows

package themecode

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"golang.org/x/sys/windows/registry"
)

func DetectSystemTheme() fyne.ThemeVariant {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		log.Println("Could not open registry key, falling back to dark theme:", err)
		return theme.VariantDark
	}
	defer key.Close()
	lightThemeVal, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		log.Println("Could not read registry value, falling back to dark theme:", err)
		return theme.VariantDark
	}
	if lightThemeVal == 1 {
		return theme.VariantLight
	}
	return theme.VariantDark
}
