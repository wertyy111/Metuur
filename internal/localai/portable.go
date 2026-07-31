package localai

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	portableModelFile = "qwen2.5-coder-0.5b-instruct-q4_0.gguf"
	portableModelURL  = "https://huggingface.co/Qwen/Qwen2.5-Coder-0.5B-Instruct-GGUF/resolve/main/qwen2.5-coder-0.5b-instruct-q4_0.gguf"
	llamaLatestAPI    = "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest"
)

type PortablePaths struct {
	Root     string
	Server   string
	Model    string
	Version  string
	Download string
}

func Paths(root string) PortablePaths {
	return PortablePaths{
		Root:     root,
		Server:   filepath.Join(root, "llama", "llama-server.exe"),
		Model:    filepath.Join(root, "models", portableModelFile),
		Version:  filepath.Join(root, "llama", "version.txt"),
		Download: filepath.Join(root, "downloads"),
	}
}

func PortableReady(root string) bool {
	paths := Paths(root)
	return regularFile(paths.Server) && regularFile(paths.Model)
}

func SetupPortable(ctx context.Context, root string, progress io.Writer) error {
	if runtime.GOOS != "windows" {
		return errors.New("portable AI setup supports Windows only")
	}
	if progress == nil {
		progress = io.Discard
	}
	paths := Paths(root)
	if err := os.MkdirAll(paths.Download, 0o755); err != nil {
		return err
	}
	if !regularFile(paths.Server) {
		tag, assetURL, err := latestLlamaCPUAsset(ctx)
		if err != nil {
			return fmt.Errorf("find llama.cpp Windows build: %w", err)
		}
		archivePath := filepath.Join(paths.Download, "llama-win-cpu-x64.zip")
		fmt.Fprintf(progress, "llama.cpp %s (Windows CPU):\n", tag)
		if err := downloadFile(ctx, assetURL, archivePath, progress); err != nil {
			return fmt.Errorf("download llama.cpp: %w", err)
		}
		if err := extractZip(archivePath, filepath.Dir(paths.Server)); err != nil {
			return fmt.Errorf("extract llama.cpp: %w", err)
		}
		_ = os.Remove(archivePath)
		if !regularFile(paths.Server) {
			return errors.New("llama-server.exe is missing from the downloaded archive")
		}
		_ = os.WriteFile(paths.Version, []byte(tag+"\n"), 0o644)
	}
	if !regularFile(paths.Model) {
		if err := os.MkdirAll(filepath.Dir(paths.Model), 0o755); err != nil {
			return err
		}
		fmt.Fprintln(progress, "Qwen2.5-Coder 0.5B Q4_0 (~409 MB):")
		if err := downloadFile(ctx, portableModelURL, paths.Model, progress); err != nil {
			return fmt.Errorf("download Qwen model: %w", err)
		}
	}
	fmt.Fprintln(progress, "Portable local completion engine is installed.")
	return nil
}

func StartPortable(root, endpoint, model string) error {
	paths := Paths(root)
	if !PortableReady(root) {
		return errors.New("portable AI is not installed; run `metuur ai setup`")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	_, statusErr := CheckOpenAI(ctx, endpoint, model)
	cancel()
	if statusErr == nil {
		return nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return errors.New("portable AI endpoint must use localhost")
	}
	port := parsed.Port()
	if port == "" {
		port = "11435"
	}
	threads := min(4, max(1, runtime.NumCPU()-1))
	arguments := []string{
		"--model", paths.Model,
		"--host", "127.0.0.1",
		"--port", port,
		"--ctx-size", "2048",
		"--threads", strconv.Itoa(threads),
		"--alias", model,
	}
	command := exec.Command(paths.Server, arguments...)
	command.Dir = filepath.Dir(paths.Server)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	logPath := filepath.Join(root, "llama-server.log")
	logFlags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, statErr := os.Stat(logPath); statErr == nil && info.Size() > 1024*1024 {
		logFlags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	logFile, logErr := os.OpenFile(logPath, logFlags, 0o600)
	if logErr == nil {
		command.Stdout = logFile
		command.Stderr = logFile
	}
	if err := command.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return fmt.Errorf("start portable AI: %w", err)
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	if err := command.Process.Release(); err != nil {
		return err
	}
	// llama.cpp binds the port only after mapping the model. Avoid probing the
	// HTTP server during that short initialization window on Windows.
	time.Sleep(800 * time.Millisecond)
	return nil
}

func WaitForProvider(ctx context.Context, provider, endpoint, model string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		probe, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		status, err := CheckProvider(probe, provider, endpoint, model)
		cancel()
		if err == nil && status.Online && status.HasModel {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func latestLlamaCPUAsset(ctx context.Context) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llamaLatestAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Metuur")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub returned %d", response.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4*1024*1024)).Decode(&release); err != nil {
		return "", "", err
	}
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, "-bin-win-cpu-x64.zip") {
			return release.TagName, asset.URL, nil
		}
	}
	return "", "", errors.New("CPU x64 archive was not found in the latest release")
}

func downloadFile(ctx context.Context, sourceURL, destination string, progress io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "Metuur")
	response, err := (&http.Client{Timeout: 30 * time.Minute}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", response.StatusCode)
	}
	part := destination + ".part"
	file, err := os.Create(part)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, &downloadProgress{reader: response.Body, total: response.ContentLength, output: progress})
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(part)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(part)
		return closeErr
	}
	if response.ContentLength > 0 && written != response.ContentLength {
		_ = os.Remove(part)
		return fmt.Errorf("incomplete download: got %d of %d bytes", written, response.ContentLength)
	}
	if err := os.Rename(part, destination); err != nil {
		_ = os.Remove(part)
		return err
	}
	fmt.Fprintln(progress)
	return nil
}

type downloadProgress struct {
	reader  io.Reader
	total   int64
	read    int64
	lastPct int64
	output  io.Writer
}

func (p *downloadProgress) Read(buffer []byte) (int, error) {
	n, err := p.reader.Read(buffer)
	p.read += int64(n)
	if p.total > 0 {
		percent := p.read * 100 / p.total
		if percent >= p.lastPct+5 || percent == 100 {
			p.lastPct = percent
			fmt.Fprintf(p.output, "\r  %3d%%  %.1f / %.1f MB", percent, float64(p.read)/(1024*1024), float64(p.total)/(1024*1024))
		}
	}
	return n, err
}

func extractZip(archivePath, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for _, entry := range archive.File {
		target := filepath.Join(root, filepath.FromSlash(entry.Name))
		absolute, err := filepath.Abs(target)
		if err != nil || (absolute != root && !strings.HasPrefix(absolute, root+string(os.PathSeparator))) {
			return fmt.Errorf("unsafe archive entry %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(absolute, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return err
		}
		input, err := entry.Open()
		if err != nil {
			return err
		}
		output, err := os.Create(absolute)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeOutErr := output.Close()
		closeInErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		if closeInErr != nil {
			return closeInErr
		}
	}
	return nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
