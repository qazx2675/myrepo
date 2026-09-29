package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vcportal/internal/collect"
)

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

type manifestVC struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	URL          string         `json:"url"`
	InstanceUUID string         `json:"instanceUuid"`
	Version      string         `json:"version"`
	Build        string         `json:"build"`
	Status       string         `json:"status"`
	Error        string         `json:"error"`
	CollectedAt  string         `json:"collectedAt"`
	Counts       collect.Counts `json:"counts"`
}

func saveCache(dir, id string, r *collect.Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	p := filepath.Join(dir, id+".json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// loadCache 는 직전 성공 결과를 읽는다. 없거나 깨졌으면 nil.
func loadCache(dir, id string) *collect.Result {
	b, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil
	}
	var r collect.Result
	if json.Unmarshal(b, &r) != nil || len(r.Detail) == 0 {
		return nil
	}
	return &r
}

func writeDetail(outDir, id string, r *collect.Result) error {
	var sb strings.Builder
	sb.WriteString("window.VCP_DATA = window.VCP_DATA || {};\n")
	fmt.Fprintf(&sb, "window.VCP_DATA[%q] = ", id)
	sb.Write(r.Detail)
	sb.WriteString(";\n")
	return os.WriteFile(filepath.Join(outDir, id+".js"), []byte(sb.String()), 0o644)
}

func writeIndexAndManifest(outDir string, index [][]string, entries []manifestVC) error {
	var sb strings.Builder
	sb.WriteString("window.VCP_INDEX = [\n")
	for i, row := range index {
		b, err := json.Marshal(row)
		if err != nil {
			return err
		}
		sb.Write(b)
		if i < len(index)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("];\n")
	if err := os.WriteFile(filepath.Join(outDir, "index.js"), []byte(sb.String()), 0o644); err != nil {
		return err
	}

	m, err := json.MarshalIndent(map[string]any{
		"generated": time.Now().Format(time.RFC3339),
		"vcenters":  entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "manifest.js"), []byte("window.VCP_MANIFEST = "+string(m)+";\n"), 0o644)
}

// publish 는 files 를 순서대로 output_dir/data 에 임시 이름으로 복사한 뒤 rename 으로 교체한다.
func publish(outDir, outputDir string, files []string) error {
	dataDir := filepath.Join(outputDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	for _, name := range files {
		if err := copyReplace(filepath.Join(outDir, name), filepath.Join(dataDir, name)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func copyReplace(src, dst string) error {
	tmp := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
