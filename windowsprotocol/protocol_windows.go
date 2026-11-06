//go:build windows

package windowsprotocol

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	protocolScheme = "nekoria-player"
	appName        = "Nekoria"
)

func getAppDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, appName), nil
}

func copySelfToAppDir(appDir string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	targetPath := filepath.Join(appDir, appName+".exe")
	if filepath.Clean(exePath) == filepath.Clean(targetPath) {
		return targetPath, nil
	}

	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to make app dir: %w", err)
	}

	srcFile, err := os.Open(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source exe: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create target exe: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return "", fmt.Errorf("failed to copy exe: %w", err)
	}

	return targetPath, nil
}

func RegisterWindowsProtocol() error {
	appDir, err := getAppDir()
	if err != nil {
		return fmt.Errorf("failed to get app dir: %w", err)
	}

	copiedExe, err := copySelfToAppDir(appDir)
	if err != nil {
		return fmt.Errorf("failed to copy bootstrapper: %w", err)
	}

	fmt.Printf("Registering protocol handler for %s using %s\n", protocolScheme, copiedExe)

	classesKey, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.CREATE_SUB_KEY)
	if err != nil {
		return fmt.Errorf("failed to open Classes key: %w", err)
	}
	defer classesKey.Close()

	protocolKey, _, err := registry.CreateKey(classesKey, protocolScheme, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("failed to create protocol key: %w", err)
	}
	defer protocolKey.Close()

	if err := protocolKey.SetStringValue("", "URL: Nekoria Protocol"); err != nil {
		return fmt.Errorf("failed to set default value: %w", err)
	}

	if err := protocolKey.SetStringValue("URL Protocol", ""); err != nil {
		return fmt.Errorf("failed to set URL Protocol value: %w", err)
	}

	defaultIconKey, _, err := registry.CreateKey(protocolKey, "DefaultIcon", registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("failed to create DefaultIcon key: %w", err)
	}
	defer defaultIconKey.Close()

	iconPath := fmt.Sprintf(`"%s",0`, copiedExe)
	if err := defaultIconKey.SetStringValue("", iconPath); err != nil {
		return fmt.Errorf("failed to set icon path: %w", err)
	}

	shellKey, _, err := registry.CreateKey(protocolKey, "shell", registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("failed to create shell key: %w", err)
	}
	defer shellKey.Close()

	openKey, _, err := registry.CreateKey(shellKey, "open", registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("failed to create open key: %w", err)
	}
	defer openKey.Close()

	commandKey, _, err := registry.CreateKey(openKey, "command", registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("failed to create command key: %w", err)
	}
	defer commandKey.Close()

	commandValue := fmt.Sprintf(`"%s" "%%1"`, copiedExe)
	if err := commandKey.SetStringValue("", commandValue); err != nil {
		return fmt.Errorf("failed to set command value: %w", err)
	}

	fmt.Printf("Successfully registered protocol handler\n")
	return nil
}
