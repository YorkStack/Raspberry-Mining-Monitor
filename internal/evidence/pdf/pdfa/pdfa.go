// Package pdfa applies and validates PDF/A conformance with an external tool
// when one is available, degrading gracefully when it is not.
//
// True PDF/A-2b/3b conformance and validation need a dedicated tool (Ghostscript
// to convert, veraPDF to validate). When neither is on PATH, the PDF is still
// produced and the status is reported as not validated, so the report and audit
// record the fact rather than silently claiming conformance.
package pdfa

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Result describes the PDF/A outcome.
type Result struct {
	Validated bool
	Status    string // human-readable status for the report and audit log
}

func toolPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// Validate checks a PDF for PDF/A conformance using veraPDF if present. It never
// fails hard: an absent tool yields a not-validated status.
func Validate(pdfPath string) Result {
	vera := toolPath("verapdf")
	if vera == "" {
		return Result{Validated: false, Status: "not validated (no PDF/A validator installed)"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, vera, "--format", "text", "-f", "2b", pdfPath).CombinedOutput()
	text := string(out)
	if err == nil && (strings.Contains(text, "PASS") || strings.Contains(text, "compliant") || strings.Contains(text, "isCompliant=\"true\"")) {
		return Result{Validated: true, Status: "validated (PDF/A-2b, veraPDF)"}
	}
	return Result{Validated: false, Status: "validation failed or non-compliant (veraPDF)"}
}

// ConvertToPDFA best-effort converts src to a PDF/A at dst using Ghostscript,
// returning ok=false (and leaving src usable) when gs is absent. Wiring this in
// is optional; the report is legible either way.
func ConvertToPDFA(src, dst string) (ok bool) {
	gs := toolPath("gs")
	if gs == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gs,
		"-dPDFA=2", "-dBATCH", "-dNOPAUSE", "-sColorConversionStrategy=UseDeviceIndependentColor",
		"-sDEVICE=pdfwrite", "-dPDFACompatibilityPolicy=1", "-sOutputFile="+dst, src)
	return cmd.Run() == nil
}
