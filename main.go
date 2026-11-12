package main

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/tls"
	_ "embed"
	"encoding/hex"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/NekoriaRevival/NekoriaBootstrapper/clientlaunchcalls"
	"github.com/NekoriaRevival/NekoriaBootstrapper/themecode"
	"github.com/NekoriaRevival/NekoriaBootstrapper/windowsprotocol"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed Nekoria.png
var logoBytes []byte

const (
	setupURL       = "setup.nekoria.pl"
	baseURL        = "nekoria.pl"
	appName        = "Nekoria"
	protocolScheme = "nekoria-player"
	authURLDefault = "http://www.nekoria.pl/Login/Negotiate.ashx"
)

var setupHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

type greenTheme struct {
	fyne.Theme
}

func (m *greenTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		if variant == theme.VariantDark {
			return color.NRGBA{R: 0x6B, G: 0xE4, B: 0x8F, A: 0xFF}
		}
		return color.NRGBA{R: 0x27, G: 0xB5, B: 0x3D, A: 0xFF}
	}
	return m.Theme.Color(name, variant)
}

type LaunchOptions struct {
	LaunchMode string
	Script     string
	AuthTicket string
	ClientYear string
}

func copySelfToAppDir(appDir string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	targetPath := filepath.Join(appDir, appName+".exe")
	if runtime.GOOS != "windows" {
		targetPath = filepath.Join(appDir, appName)
	}

	if filepath.Clean(exePath) == filepath.Clean(targetPath) {
		return targetPath, nil
	}

	srcFile, err := os.Open(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source exe: %w", err)
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", fmt.Errorf("failed to make target dir: %w", err)
	}

	destFile, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create target exe: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return "", fmt.Errorf("failed to copy exe: %w", err)
	}

	if runtime.GOOS != "windows" {
		os.Chmod(targetPath, 0755)
	}

	return targetPath, nil
}

func main() {
	launchOpts, err := parseLaunchOptions()
	if err != nil {
		fmt.Printf("Argument parsing error: %v\n", err)
	}

	myApp := app.New()
	myWindow := myApp.NewWindow("Nekoria Installer")

	detectedVariant := themecode.DetectSystemTheme()
	var baseTheme fyne.Theme
	if detectedVariant == theme.VariantLight {
		baseTheme = theme.LightTheme()
	} else {
		baseTheme = theme.DarkTheme()
	}
	myApp.Settings().SetTheme(&greenTheme{Theme: baseTheme})

	logoImage := canvas.NewImageFromResource(fyne.NewStaticResource("logo.png", logoBytes))
	logoImage.FillMode = canvas.ImageFillContain
	logoImage.SetMinSize(fyne.NewSize(96, 96))

	statusLabel := widget.NewLabel("Preparing to install...")
	statusLabel.Alignment = fyne.TextAlignCenter

	customLoader, track, chunkToAnimate := createCustomLoader()
	cancelButton := widget.NewButton("Cancel", func() { myApp.Quit() })

	verticalSpacer := canvas.NewRectangle(color.Transparent)
	verticalSpacer.SetMinSize(fyne.NewSize(0, 12))

	content := container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(logoImage),
		statusLabel,
		customLoader,
		verticalSpacer,
		container.NewCenter(cancelButton),
		layout.NewSpacer(),
	)

	myWindow.SetContent(content)
	myWindow.Resize(fyne.NewSize(440, 280))
	myWindow.SetFixedSize(true)
	myWindow.CenterOnScreen()

	loaderWidth := float32(440) - theme.Padding()*4
	go runInstallerLogic(launchOpts, statusLabel, customLoader, track, chunkToAnimate, loaderWidth, cancelButton, myWindow)

	myWindow.ShowAndRun()
}

func createCustomLoader() (fyne.CanvasObject, *canvas.Rectangle, *canvas.Rectangle) {
	track := canvas.NewRectangle(theme.DisabledColor())
	chunk := canvas.NewRectangle(theme.PrimaryColor())
	loader := container.NewWithoutLayout(track, chunk)
	return loader, track, chunk
}

func animateIndeterminate(chunk *canvas.Rectangle, totalWidth float32, stop chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	chunkWidth := float32(80)
	fyne.Do(func() {
		chunk.Resize(fyne.NewSize(chunkWidth, 8))
	})
	var xPos float32 = -chunkWidth
	for {
		select {
		case <-stop:
			return
		default:
			xPos += 4
			if xPos > totalWidth {
				xPos = -chunkWidth
			}
			position := fyne.NewPos(xPos, 0)
			fyne.Do(func() {
				chunk.Move(position)
			})
			time.Sleep(16 * time.Millisecond)
		}
	}
}

func parseLaunchOptions() (LaunchOptions, error) {
	opts := LaunchOptions{LaunchMode: "install"}

	if len(os.Args) <= 1 {
		return opts, nil
	}

	firstArg := strings.Trim(os.Args[1], `"'`)

	if strings.HasPrefix(firstArg, protocolScheme+":") {
		normalized := firstArg
		if !strings.HasPrefix(firstArg, protocolScheme+"://") {
			normalized = strings.Replace(firstArg, protocolScheme+":", protocolScheme+"://", 1)
		}
		return parseProtocolArgs(normalized)
	}

	if firstArg == "-play" {
		return parseCommandLineArgs(os.Args)
	}

	return opts, nil
}

func parseProtocolArgs(arg string) (LaunchOptions, error) {
	opts := LaunchOptions{}
	trimmed := strings.TrimPrefix(arg, protocolScheme+"://")

	parts := strings.Split(trimmed, "+")
	for _, part := range parts {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 {
			key, value := kv[0], kv[1]
			switch key {
			case "launchmode":
				opts.LaunchMode = value
			case "placelauncherurl":
				decodedValue, err := url.QueryUnescape(value)
				if err != nil {
					return opts, fmt.Errorf("invalid placelauncherurl: %w", err)
				}
				opts.Script = decodedValue
			case "gameinfo":
				opts.AuthTicket = value
			case "clientyear":
				opts.ClientYear = value
			}
		}
	}

	if opts.LaunchMode != "play" {
		return opts, fmt.Errorf("unsupported launch mode: %s", opts.LaunchMode)
	}

	return opts, nil
}

func parseCommandLineArgs(args []string) (LaunchOptions, error) {
	opts := LaunchOptions{LaunchMode: "play"}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-play":
			opts.LaunchMode = "play"
		case "-script":
			if i+1 < len(args) {
				opts.Script = args[i+1]
				i++
			}
		case "-ticket":
			if i+1 < len(args) {
				opts.AuthTicket = args[i+1]
				i++
			}
		case "-clientyear":
			if i+1 < len(args) {
				opts.ClientYear = args[i+1]
				i++
			}
		}
	}
	return opts, nil
}

func getLatestClientVersion() (string, error) {
	resp, err := setupHTTPClient.Get(fmt.Sprintf("https://%s/version", setupURL))
	if err != nil {
		return "", fmt.Errorf("failed to fetch version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}

func getSHA1Hash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkAndInstallClient(appDir string, onProgress func(string, float32)) error {
	onProgress("Fetching latest client version...", 0)

	latestVersion, err := getLatestClientVersion()
	if err != nil {
		return fmt.Errorf("failed to get latest version: %w", err)
	}

	fmt.Printf("Latest client version: %s\n", latestVersion)

	versionsDir := filepath.Join(appDir, "Versions")
	if err := os.MkdirAll(versionsDir, 0755); err != nil {
		return err
	}

	currentVersionDir := filepath.Join(versionsDir, latestVersion)
	versionFilePath := filepath.Join(currentVersionDir, "version.txt")

	needsUpdate := false
	installedVersion := ""

	if data, err := os.ReadFile(versionFilePath); err == nil {
		installedVersion = strings.TrimSpace(string(data))
		if installedVersion != latestVersion {
			fmt.Printf("Version mismatch: installed=%s, latest=%s\n", installedVersion, latestVersion)
			needsUpdate = true
		}
	} else {
		needsUpdate = true
	}

	if needsUpdate && installedVersion != "" {
		oldVersionDir := filepath.Join(versionsDir, installedVersion)
		if _, err := os.Stat(oldVersionDir); err == nil {
			fmt.Printf("Removing old version directory: %s\n", installedVersion)
			os.RemoveAll(oldVersionDir)
		}
	}

	if err := os.MkdirAll(currentVersionDir, 0755); err != nil {
		return err
	}

	clientYears := []string{"2016"}

	needsInstall := needsUpdate
	if !needsInstall {
		for _, year := range clientYears {
			clientDir := filepath.Join(currentVersionDir, fmt.Sprintf("Client%s", year))
			exePath := filepath.Join(clientDir, "NekoriaPlayerBeta.exe")

			if _, err := os.Stat(exePath); os.IsNotExist(err) {
				needsInstall = true
				break
			}
		}
	}

	if !needsInstall {
		fmt.Println("All clients already installed and valid")
		onProgress("Client is up to date", 1)
		return nil
	}

	fmt.Println("Downloading latest client files...")

	entries, err := os.ReadDir(currentVersionDir)
	if err == nil {
		for _, entry := range entries {
			path := filepath.Join(currentVersionDir, entry.Name())
			if !strings.Contains(entry.Name(), "Bootstrapper") &&
				!strings.Contains(entry.Name(), "Launcher") &&
				entry.Name() != "version.txt" {
				if entry.IsDir() {
					os.RemoveAll(path)
				} else {
					os.Remove(path)
				}
			}
		}
	}

	if err := os.WriteFile(versionFilePath, []byte(latestVersion), 0644); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}

	totalClients := len(clientYears)
	for i, year := range clientYears {
		baseProgress := float32(i) / float32(totalClients)
		clientProgress := 1.0 / float32(totalClients)

		onProgress(fmt.Sprintf("Downloading %s client...", year), baseProgress)

		downloadURL := fmt.Sprintf("https://%s/%s-%sClient.zip", setupURL, latestVersion, year)
		zipPath, err := downloadClientZip(downloadURL, func(p float32) {
			onProgress(fmt.Sprintf("Downloading %s client...", year), baseProgress+(p*clientProgress*0.7))
		})
		if err != nil {
			return fmt.Errorf("failed to download %s client: %w", year, err)
		}

		onProgress(fmt.Sprintf("Extracting %s client...", year), baseProgress+(clientProgress*0.7))

		clientDir := filepath.Join(currentVersionDir, fmt.Sprintf("Client%s", year))
		if err := os.MkdirAll(clientDir, 0755); err != nil {
			os.Remove(zipPath)
			return err
		}

		if err := unzipToDir(zipPath, clientDir); err != nil {
			os.Remove(zipPath)
			return fmt.Errorf("failed to extract %s client: %w", year, err)
		}

		os.Remove(zipPath)
		onProgress(fmt.Sprintf("Installed %s client", year), baseProgress+clientProgress)
	}

	onProgress("Finalizing installation...", 0.95)

	entries, err = os.ReadDir(versionsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != latestVersion {
				oldPath := filepath.Join(versionsDir, entry.Name())
				fmt.Printf("Removing old version: %s\n", entry.Name())
				os.RemoveAll(oldPath)
			}
		}
	}

	onProgress("Installation complete!", 1)
	return nil
}

func downloadClientZip(urlStr string, onProgress func(float32)) (string, error) {
	client := setupHTTPClient
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.CreateTemp("", "client-*.zip")
	if err != nil {
		return "", err
	}
	defer out.Close()

	writer := &ProgressWriter{
		Total:      resp.ContentLength,
		File:       out,
		OnProgress: onProgress,
	}

	_, err = io.Copy(writer, resp.Body)
	return out.Name(), err
}

func unzipToDir(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func runInstallerLogic(opts LaunchOptions, label *widget.Label, cLoader fyne.CanvasObject, track *canvas.Rectangle, chunk *canvas.Rectangle, loaderWidth float32, btn *widget.Button, win fyne.Window) {
	var wg sync.WaitGroup
	stopAnimation := make(chan bool, 1)
	isAnimating := true

	fyne.Do(func() {
		track.Move(fyne.NewPos(0, 0))
		track.Resize(fyne.NewSize(loaderWidth, 8))
		track.Refresh()
	})

	wg.Add(1)
	go animateIndeterminate(chunk, loaderWidth, stopAnimation, &wg)

	appDir, err := getAppDir()
	if err != nil {
		fyne.Do(func() {
			label.SetText(fmt.Sprintf("Error: %v", err))
		})
		return
	}

	fyne.Do(func() {
		label.SetText("Checking for client installation...")
	})

	var progressMu sync.Mutex
	progressCallback := func(msg string, progress float32) {
		progressMu.Lock()
		defer progressMu.Unlock()

		if isAnimating && progress > 0 {
			select {
			case stopAnimation <- true:
			default:
			}
			wg.Wait()
			isAnimating = false
		}

		if progress < 0 {
			progress = 0
		}
		if progress > 1 {
			progress = 1
		}
		fyne.Do(func() {
			label.SetText(msg)
			track.Move(fyne.NewPos(0, 0))
			track.Resize(fyne.NewSize(loaderWidth, 8))
			track.Refresh()
			chunk.Resize(fyne.NewSize(loaderWidth*progress, 8))
			chunk.Move(fyne.NewPos(0, 0))
			chunk.Refresh()
		})
	}

	if err := checkAndInstallClient(appDir, progressCallback); err != nil {
		fyne.Do(func() {
			label.SetText(fmt.Sprintf("Failed to install client: %v", err))
		})
		if isAnimating {
			select {
			case stopAnimation <- true:
			default:
			}
			wg.Wait()
		}
		return
	}

	if !isAnimating {
		isAnimating = true
		stopAnimation = make(chan bool, 1)
		fyne.Do(func() {
			track.Move(fyne.NewPos(0, 0))
			track.Resize(fyne.NewSize(loaderWidth, 8))
			track.Refresh()
		})
		wg.Add(1)
		go animateIndeterminate(chunk, loaderWidth, stopAnimation, &wg)
	}
	if opts.LaunchMode != "play" {
		if err := createDesktopFile(); err != nil {
			fmt.Printf("Warning: could not create desktop file: %v\n", err)
		}
	}

	if err := windowsprotocol.RegisterWindowsProtocol(); err != nil {
		fmt.Printf("Warning: could not register Windows protocol: %v\n", err)
	}

	if opts.LaunchMode == "play" {
		fyne.Do(func() {
			label.SetText("Starting Nekoria...")
		})
		if err := launchClient(appDir, opts); err != nil {
			fyne.Do(func() {
				label.SetText(fmt.Sprintf("Failed to launch: %v", err))
			})
			if isAnimating {
				select {
				case stopAnimation <- true:
				default:
				}
				wg.Wait()
				isAnimating = false
			}
			return
		}
	}

	if isAnimating && opts.LaunchMode != "play" {
		select {
		case stopAnimation <- true:
		default:
		}
		wg.Wait()
		isAnimating = false
	}

	if opts.LaunchMode != "play" {
		fyne.Do(func() {
			label.SetText("Nekoria is ready!")
			cLoader.Hide()
			btn.SetText("Finish")
			btn.OnTapped = func() { win.Close() }
			btn.Refresh()
		})
	} else {
		go func() {
			time.Sleep(5 * time.Second)
			fyne.Do(func() {
				win.Close()
			})
		}()
	}
}

func getAppDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appPath := filepath.Join(configDir, appName)
	return appPath, os.MkdirAll(appPath, 0755)
}

type ProgressWriter struct {
	Total      int64
	Written    int64
	File       *os.File
	OnProgress func(float32)
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.File.Write(p)
	if err == nil {
		pw.Written += int64(n)
		if pw.Total > 0 {
			progress := float32(pw.Written) / float32(pw.Total)
			pw.OnProgress(progress)
		}
	}
	return n, err
}

func createDesktopFile() error {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		return nil
	}

	appDir, err := getAppDir()
	if err != nil {
		return err
	}

	copiedExe, err := copySelfToAppDir(appDir)
	if err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	desktopFilePath := filepath.Join(homeDir, ".local", "share", "applications", "nekoria-player.desktop")

	iconDir := filepath.Join(homeDir, ".local", "share", "icons")
	if err := os.MkdirAll(iconDir, 0755); err != nil {
		return err
	}
	iconPath := filepath.Join(iconDir, "Nekoria.png")
	if err := os.WriteFile(iconPath, logoBytes, 0644); err != nil {
		return err
	}

	desktopContent := fmt.Sprintf(`[Desktop Entry]
Name=Nekoria
Comment=Nekoria Launcher
Exec="%s" %%u
Icon=%s
Terminal=false
Type=Application
Categories=Game;
MimeType=x-scheme-handler/%s;
`, copiedExe, iconPath, protocolScheme)

	if err := os.WriteFile(desktopFilePath, []byte(desktopContent), 0644); err != nil {
		return err
	}

	go func() {
		loader, wineEnv := nkWine()
		for _, v := range []string{"Auto", "ShowCrashDialog"} {
			cmd := exec.Command(loader, "reg", "add", `HKCU\Software\Wine\Winedbg`,
				"/v", v, "/t", "REG_DWORD", "/d", "0", "/f")
			cmd.Env = wineEnv
			clientlaunchcalls.SetupProcAttr(cmd)
			if runtime.GOOS == "freebsd" {
				if dn, derr := os.OpenFile(os.DevNull, os.O_RDWR, 0); derr == nil {
					cmd.Stdout, cmd.Stderr = dn, dn
				}
			}
			_ = cmd.Run()
		}
	}()

	return nil
}

func nkWineEnv() []string {
	env := os.Environ()
	if os.Getenv("WINEPREFIX") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			env = append(env, "WINEPREFIX="+filepath.Join(home, ".wine-nekoria"))
		}
	}
	if os.Getenv("WINEDEBUG") == "" {
		if runtime.GOOS == "freebsd" {
			env = append(env, "WINEDEBUG=+relay")
		} else {
			env = append(env, "WINEDEBUG=-all")
		}
	}
	if runtime.GOOS == "freebsd" && os.Getenv("WINEDLLOVERRIDES") == "" {
		env = append(env, "WINEDLLOVERRIDES=winedbg.exe=d")
	}
	return env
}

func healLLVMSymlink(lib, llvm string) {
	link := filepath.Join(lib, "libLLVM.so.19.1")
	target := filepath.Join(llvm, "libLLVM.so.19.1")
	if _, terr := os.Stat(target); terr != nil {
		return
	}
	if cur, lerr := os.Readlink(link); lerr == nil && cur == target {
		return
	}
	_ = os.Remove(link)
	_ = os.Symlink(target, link)
}

func nkWine() (string, []string) {
	if runtime.GOOS == "freebsd" && os.Getenv("WINEPREFIX") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root := filepath.Join(home, ".i386-wine-pkg", "usr", "local")
			lib := filepath.Join(root, "lib")
			llvm := filepath.Join(root, "llvm19", "lib")
			loader := filepath.Join(root, "bin", "wine")
			if _, lerr := os.Stat(loader); lerr == nil {
				if _, derr := os.Stat(lib); derr == nil {
					healLLVMSymlink(lib, llvm)
					env := append(os.Environ(),
						"WINEPREFIX="+filepath.Join(home, ".i386nekoria11"))
					if os.Getenv("LD_32_LIBRARY_PATH") == "" {
						env = append(env, "LD_32_LIBRARY_PATH="+lib+":"+filepath.Join(lib, "pulseaudio")+":"+llvm)
					}
					if os.Getenv("LIBGL_DRIVERS_PATH") == "" {
						env = append(env, "LIBGL_DRIVERS_PATH="+filepath.Join(lib, "dri"))
					}
					if os.Getenv("WINEDEBUG") == "" {
						env = append(env, "WINEDEBUG=-all")
					}
					if os.Getenv("WINEDLLOVERRIDES") == "" {
						env = append(env, "WINEDLLOVERRIDES=winedbg.exe=d")
					}
					return loader, env
				}
			}
		}
	}
	return "wine", nkWineEnv()
}

func launchClient(appDir string, opts LaunchOptions) error {
	latestVersion, err := getLatestClientVersion()
	if err != nil {
		return fmt.Errorf("failed to get latest version: %w", err)
	}

	clientYear := opts.ClientYear
	if clientYear == "" {
		clientYear = "2016"
	}

	clientFolder := fmt.Sprintf("Client%s", clientYear)
	exeName := "NekoriaPlayerBeta.exe"
	exePath := filepath.Join(appDir, "Versions", latestVersion, clientFolder, exeName)

	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("client executable not found: %s", exePath)
	}

	var args []string
	if opts.LaunchMode == "play" {
		args = append(args, "--play")
		args = append(args, "-a", authURLDefault)
		args = append(args, "-t", opts.AuthTicket)
		args = append(args, "-j", opts.Script)
	}

	fmt.Printf("Launching client with command:\n%s %s\n", exePath, strings.Join(args, " "))

	var cmd *exec.Cmd
	loader := ""
	if runtime.GOOS == "windows" {
		cmd = exec.Command(exePath, args...)
	} else {
		allArgs := append([]string{exePath}, args...)
		var wineEnv []string
		loader, wineEnv = nkWine()
		cmd = exec.Command(loader, allArgs...)
		cmd.Env = wineEnv
	}
	clientlaunchcalls.SetupProcAttr(cmd)

	if runtime.GOOS != "windows" {
		if old, oerr := filepath.Glob(filepath.Join(appDir, "client-*.log")); oerr == nil && len(old) > 2 {
			sort.Slice(old, func(i, j int) bool {
				si, e1 := os.Stat(old[i])
				sj, e2 := os.Stat(old[j])
				if e1 != nil || e2 != nil {
					return false
				}
				return si.ModTime().After(sj.ModTime())
			})
			for _, p := range old[2:] {
				os.Remove(p)
			}
		}
		if runtime.GOOS == "freebsd" && loader == "wine" {
			if dn, derr := os.OpenFile(os.DevNull, os.O_RDWR, 0); derr == nil {
				cmd.Stdout, cmd.Stderr = dn, dn
			}
		} else if logf, lerr := os.CreateTemp(appDir, "client-*.log"); lerr == nil {
			cmd.Stdout, cmd.Stderr = logf, logf
		}
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}

	if cmd.Process == nil {
		return fmt.Errorf("process started but process handle is nil")
	}

	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("started client but failed to detach (release): %w", err)
	}

	return nil
}
