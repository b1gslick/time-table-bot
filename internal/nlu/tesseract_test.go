package nlu

import (
	"context"
	"strings"
	"testing"
)

func TestTesseractRecognizerReadsLocalOCR(t *testing.T) {
	dir := t.TempDir()
	tesseractPath := writeExecutable(t, dir, "tesseract", `#!/bin/sh
case "$1" in
  *.png) ;;
  *) echo "unexpected extension" >&2; exit 1 ;;
esac
printf 'level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n'
printf '5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t95\tхочу\n'
printf '5\t1\t1\t1\t1\t2\t0\t0\t10\t10\t94\tэпиляцию\n'
printf '5\t1\t1\t1\t1\t3\t0\t0\t10\t10\t93\tзавтра\n'
printf '5\t1\t1\t1\t1\t4\t0\t0\t10\t10\t92\tвечером\n'
`)
	recognizer, err := NewTesseractRecognizer(TesseractConfig{
		CLIPath:   tesseractPath,
		Languages: "rus+eng",
	})
	if err != nil {
		t.Fatalf("NewTesseractRecognizer: %v", err)
	}
	got, err := recognizer.RecognizeText(context.Background(), ImageTextRequest{
		Image:    []byte("png"),
		MIMEType: "image/png",
		Language: "ru",
	})
	if err != nil {
		t.Fatalf("RecognizeText: %v", err)
	}
	if got != "хочу эпиляцию завтра вечером" {
		t.Fatalf("recognized text = %q", got)
	}
}

func TestTesseractRecognizerReturnsConfidence(t *testing.T) {
	dir := t.TempDir()
	tesseractPath := writeExecutable(t, dir, "tesseract", `#!/bin/sh
printf 'level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n'
printf '5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t40\tзапись\n'
printf '5\t1\t1\t1\t1\t2\t0\t0\t10\t10\t60\tзавтра\n'
`)
	recognizer, err := NewTesseractRecognizer(TesseractConfig{CLIPath: tesseractPath})
	if err != nil {
		t.Fatalf("NewTesseractRecognizer: %v", err)
	}
	text, confidence, err := recognizer.RecognizeTextWithConfidence(context.Background(), ImageTextRequest{Image: []byte("jpg")})
	if err != nil {
		t.Fatalf("RecognizeTextWithConfidence: %v", err)
	}
	if text != "запись завтра" || confidence != 50 {
		t.Fatalf("result = %q, %.1f", text, confidence)
	}
}

func TestParseTesseractTSVPreservesLineOrder(t *testing.T) {
	data := []byte("level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t90\t09:30\n" +
		"5\t1\t1\t1\t1\t2\t0\t0\t10\t10\t90\tЛиза\n" +
		"5\t1\t1\t1\t2\t1\t0\t0\t10\t10\t90\tэпиляция\n")
	text, confidence, err := parseTesseractTSV(data)
	if err != nil || text != "09:30 Лиза\nэпиляция" || confidence != 90 {
		t.Fatalf("result = %q, %.1f, err=%v", text, confidence, err)
	}
}

func TestTesseractRecognizerRejectsEmptyOutput(t *testing.T) {
	dir := t.TempDir()
	tesseractPath := writeExecutable(t, dir, "tesseract", "#!/bin/sh\nexit 0\n")
	recognizer, err := NewTesseractRecognizer(TesseractConfig{CLIPath: tesseractPath})
	if err != nil {
		t.Fatalf("NewTesseractRecognizer: %v", err)
	}
	_, err = recognizer.RecognizeText(context.Background(), ImageTextRequest{Image: []byte("jpg")})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("RecognizeText error = %v", err)
	}
}

func TestImageExtension(t *testing.T) {
	if got := imageExtension("image/png; charset=binary"); got != ".png" {
		t.Fatalf("imageExtension = %q", got)
	}
}
