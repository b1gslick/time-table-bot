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

func (r *TesseractRecognizer) RecognizeTextWithLayout(ctx context.Context, req ImageTextRequest) (string, string, error) {
	text, layout, _, err := r.RecognizeTextWithLayoutAndConfidence(ctx, req)
	return text, layout, err
}

func (r *TesseractRecognizer) RecognizeTextWithLayoutAndConfidence(ctx context.Context, req ImageTextRequest) (string, string, float64, error) {
	return r.recognize(ctx, req)
}

func (r *TesseractRecognizer) RecognizeTextWithConfidence(ctx context.Context, req ImageTextRequest) (string, float64, error) {
	text, _, confidence, err := r.recognize(ctx, req)
	return text, confidence, err
}

func (r *TesseractRecognizer) recognize(ctx context.Context, req ImageTextRequest) (string, string, float64, error) {
	if r == nil {
		return "", "", 0, fmt.Errorf("tesseract recognizer is nil")
	}
	if len(req.Image) == 0 {
		return "", "", 0, fmt.Errorf("image is required")
	}
	select {
	case r.worker <- struct{}{}:
		defer func() { <-r.worker }()
	case <-ctx.Done():
		return "", "", 0, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "time-table-bot-image-*")
	if err != nil {
		return "", "", 0, err
	}
	defer os.RemoveAll(dir)

	sourcePath := filepath.Join(dir, "source"+imageExtension(req.MIMEType))
	if err := os.WriteFile(sourcePath, req.Image, 0o600); err != nil {
		return "", "", 0, err
	}
	type result struct {
		text       string
		layoutText string
		confidence float64
		err        error
	}
	results := make([]result, 0, 2)
	for _, psm := range []string{"6", "11"} {
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, r.cliPath, sourcePath, "stdout", "-l", r.languages, "--psm", psm, "tsv")
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			results = append(results, result{err: commandError("tesseract", err, stderr.Bytes())})
			continue
		}
		text, layoutText, confidence, err := parseTesseractTSVWithLayout(stdout.Bytes())
		results = append(results, result{text: text, layoutText: layoutText, confidence: confidence, err: err})
	}
	best := result{}
	for _, candidate := range results {
		if candidate.err == nil && strings.TrimSpace(candidate.text) != "" && ocrResultScore(candidate.text, candidate.confidence) > ocrResultScore(best.text, best.confidence) {
			best = candidate
		}
	}
	if best.text == "" {
		for _, candidate := range results {
			if candidate.err != nil {
				return "", "", 0, candidate.err
			}
		}
		return "", "", 0, fmt.Errorf("tesseract output is empty")
	}
	return best.text, best.layoutText, best.confidence, nil
}

func parseTesseractTSV(data []byte) (string, float64, error) {
	text, _, confidence, err := parseTesseractTSVWithLayout(data)
	return text, confidence, err
}

type tesseractLine struct {
	words                   []string
	left, top, right, lower int
}

func parseTesseractTSVWithLayout(data []byte) (string, string, float64, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil && err != io.EOF {
		return "", "", 0, fmt.Errorf("parse tesseract output: %w", err)
	}
	var lines []tesseractLine
	var line tesseractLine
	currentLine := ""
	pageWidth, pageHeight := 0, 0
	var confidenceTotal float64
	var confidenceCount int
	for index, record := range records {
		if index == 0 || len(record) < 12 {
			continue
		}
		if record[0] == "1" {
			pageWidth, _ = strconv.Atoi(record[8])
			pageHeight, _ = strconv.Atoi(record[9])
			continue
		}
		if record[0] != "5" {
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
		lineKey := strings.Join(record[1:5], ":")
		if currentLine != "" && lineKey != currentLine {
			lines = append(lines, line)
			line = tesseractLine{}
		}
		currentLine = lineKey
		left, _ := strconv.Atoi(record[6])
		top, _ := strconv.Atoi(record[7])
		width, _ := strconv.Atoi(record[8])
		height, _ := strconv.Atoi(record[9])
		if len(line.words) == 0 || left < line.left {
			line.left = left
		}
		if len(line.words) == 0 || top < line.top {
			line.top = top
		}
		if left+width > line.right {
			line.right = left + width
		}
		if top+height > line.lower {
			line.lower = top + height
		}
		line.words = append(line.words, word)
		confidenceTotal += confidence
		confidenceCount++
	}
	if confidenceCount == 0 {
		return "", "", 0, nil
	}
	if len(line.words) > 0 {
		lines = append(lines, line)
	}
	plain := make([]string, 0, len(lines))
	layout := make([]string, 0, len(lines)+1)
	if pageWidth > 0 && pageHeight > 0 {
		layout = append(layout, fmt.Sprintf("[page width=%d height=%d]", pageWidth, pageHeight))
	}
	for _, item := range lines {
		value := strings.Join(item.words, " ")
		plain = append(plain, value)
		layout = append(layout, fmt.Sprintf("[x=%d y=%d w=%d h=%d] %s", item.left, item.top, item.right-item.left, item.lower-item.top, value))
	}
	return strings.Join(plain, "\n"), strings.Join(layout, "\n"), confidenceTotal / float64(confidenceCount), nil
}

func ocrResultScore(text string, confidence float64) float64 {
	wordCount := len(strings.Fields(text))
	if wordCount > 80 {
		wordCount = 80
	}
	return confidence + float64(wordCount)
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
