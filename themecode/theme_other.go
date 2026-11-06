//go:build !linux && !freebsd && !windows

package themecode

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

func DetectSystemTheme() fyne.ThemeVariant {
	log.Println("No theme for this OS")
	return theme.VariantDark
}
