package nlu

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type TesseractConfig struct {
	CLIPath   string
	Languages string
	Timeout   time.Duration
}

type TesseractRecognizer struct {
	cliPath   string
	languages string
	timeout   time.Duration
	worker    chan struct{}
}

func NewTesseractRecognizer(cfg TesseractConfig) (*TesseractRecognizer, error) {
	cliPath, err := executablePath(cfg.CLIPath, "tesseract")
	if err != nil {
		return nil, err
	}
	languages := strings.TrimSpace(cfg.Languages)
	if languages == "" {
		languages = "rus+eng"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return &TesseractRecognizer{
		cliPath:   cliPath,
		languages: languages,
		timeout:   timeout,
		worker:    make(chan struct{}, 1),
	}, nil
}

func (r *TesseractRecognizer) RecognizeText(ctx context.Context, req ImageTextRequest) (string, error) {
	text, _, err := r.RecognizeTextWithConfidence(ctx, req)
	return text, err
}

func (r *TesseractRecognizer) RecognizeTextWithConfidence(ctx context.Context, req ImageTextRequest) (string, float64, error) {
	if r == nil {
		return "", 0, fmt.Errorf("tesseract recognizer is nil")
	}
	if len(req.Image) == 0 {
		return "", 0, fmt.Errorf("image is required")
	}
	select {
	case r.worker <- struct{}{}:
		defer func() { <-r.worker }()
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "time-table-bot-image-*")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(dir)

	sourcePath := filepath.Join(dir, "source"+imageExtension(req.MIMEType))
	if err := os.WriteFile(sourcePath, req.Image, 0o600); err != nil {
		return "", 0, err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.cliPath, sourcePath, "stdout", "-l", r.languages, "--psm", "6", "tsv")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", 0, commandError("tesseract", err, stderr.Bytes())
	}
	text, confidence, err := parseTesseractTSV(stdout.Bytes())
	if err != nil {
		return "", 0, err
	}
	if text == "" {
		return "", 0, fmt.Errorf("tesseract output is empty")
	}
	return text, confidence, nil
}

func parseTesseractTSV(data []byte) (string, float64, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil && err != io.EOF {
		return "", 0, fmt.Errorf("parse tesseract output: %w", err)
	}
	var words []string
	var confidenceTotal float64
	var confidenceCount int
	for index, record := range records {
		if index == 0 || len(record) < 12 || record[0] != "5" {
			continue
		}
		word := strings.TrimSpace(record[11])
		if word == "" {
			continue
		}
		confidence, err := strconv.ParseFloat(record[10], 64)
		if err != nil || confidence < 0 {
			continue
		}
		words = append(words, word)
		confidenceTotal += confidence
		confidenceCount++
	}
	if confidenceCount == 0 {
		return "", 0, nil
	}
	return strings.Join(words, " "), confidenceTotal / float64(confidenceCount), nil
}

func imageExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/tiff":
		return ".tiff"
	default:
		return ".jpg"
	}
}
