// Package pdf renders the human-readable evidence report.
//
// It produces a self-contained A4 PDF: no external fonts, stylesheets or images
// are fetched. Each page carries the report id, period, revision, page number
// and the shortened evidence-bundle hash; the cover carries the full hash and a
// QR code of the report id + hash. The final PDF hash is never embedded in the
// PDF itself — it goes into the signed final manifest afterwards.
//
// Strict PDF/A-2b/3b conformance and validation are applied by an external tool
// when available (see the pdfa subpackage); when it is absent the PDF is still
// produced and the PDF/A status is reported as not validated.
package pdf

import (
	"bytes"
	"fmt"

	"github.com/go-pdf/fpdf"
	qrcode "github.com/skip2/go-qrcode"
)

// Title is the fixed report title.
const Title = "Bitcoin Mining Activity and Evidence Report"

// Disclaimer is printed on every report.
const Disclaimer = "Technical factual documentation only. This report does not " +
	"determine the legal or tax classification of the mining activity."

// KV is a labelled value for a section table.
type KV struct{ Key, Value string }

// Section is one titled block of key/value rows.
type Section struct {
	Title string
	Rows  []KV
}

// Data is everything the report renders. It is assembled from the database by
// Collect, keeping rendering independent of storage.
type Data struct {
	ReportID           string
	Period             string
	Revision           int
	EvidenceBundleHash string
	GeneratedAt        string
	SoftwareVersion    string
	GitCommit          string
	SchemaVersion      int
	PDFAStatus         string // e.g. "validated (PDF/A-2b)" or "not validated (tool unavailable)"
	SignatureStatus    string
	BackupStatus       string
	Sections           []Section
}

func shortHash(h string) string {
	if len(h) > 16 {
		return h[:16] + "…"
	}
	return h
}

// Generate renders the report to path.
func Generate(d Data, path string) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(Title, true)
	pdf.SetAuthor("Raspberry Mining Monitor — Evidence", true)
	pdf.AliasNbPages("{nb}")

	revLabel := "ORIGINAL"
	if d.Revision > 0 {
		revLabel = fmt.Sprintf("REVISION-%03d", d.Revision)
	}

	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(90, 90, 90)
		left := fmt.Sprintf("%s  |  %s  |  %s", d.ReportID, d.Period, revLabel)
		pdf.CellFormat(0, 5, left, "", 0, "L", false, 0, "")
		right := fmt.Sprintf("Page %d of {nb}  |  bundle %s", pdf.PageNo(), shortHash(d.EvidenceBundleHash))
		pdf.CellFormat(0, 5, right, "", 0, "R", false, 0, "")
	})

	// Cover page.
	pdf.AddPage()
	pdf.SetTextColor(20, 20, 20)
	pdf.SetFont("Helvetica", "B", 20)
	pdf.MultiCell(0, 9, Title, "", "L", false)
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "", 11)
	pdf.MultiCell(0, 6, fmt.Sprintf("Report %s\nReporting period %s (%s)\nGenerated %s",
		d.ReportID, d.Period, revLabel, d.GeneratedAt), "", "L", false)
	pdf.Ln(3)

	// Verification block + QR.
	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(0, 6, "Evidence integrity", "", 1, "L", false, 0, "")
	pdf.SetFont("Courier", "", 9)
	pdf.MultiCell(120, 5, "Evidence-bundle hash:\n"+d.EvidenceBundleHash, "", "L", false)

	if png, err := qrcode.Encode(d.ReportID+"|"+d.EvidenceBundleHash, qrcode.Medium, 256); err == nil {
		pdf.RegisterImageOptionsReader("qr", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
		pdf.ImageOptions("qr", 150, 70, 40, 40, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	}
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "", 9)
	pdf.MultiCell(0, 5, fmt.Sprintf("Software %s (%s)  |  DB schema v%d\nPDF/A: %s\nSignature: %s\nBackup: %s",
		d.SoftwareVersion, d.GitCommit, d.SchemaVersion, d.PDFAStatus, d.SignatureStatus, d.BackupStatus),
		"", "L", false)
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "I", 9)
	pdf.SetTextColor(120, 120, 120)
	pdf.MultiCell(0, 5, Disclaimer, "", "L", false)
	pdf.MultiCell(0, 5, "Digital original stored in the Mining Evidence Archive.", "", "L", false)

	// Content sections.
	for _, sec := range d.Sections {
		pdf.AddPage()
		pdf.SetTextColor(20, 20, 20)
		pdf.SetFont("Helvetica", "B", 13)
		pdf.CellFormat(0, 8, sec.Title, "", 1, "L", false, 0, "")
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 10)
		for _, r := range sec.Rows {
			pdf.SetFont("Helvetica", "B", 10)
			pdf.CellFormat(70, 6, r.Key, "", 0, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 10)
			pdf.MultiCell(0, 6, r.Value, "", "L", false)
		}
		if len(sec.Rows) == 0 {
			pdf.SetFont("Helvetica", "I", 10)
			pdf.SetTextColor(120, 120, 120)
			pdf.CellFormat(0, 6, "No data recorded for this period.", "", 1, "L", false, 0, "")
			pdf.SetTextColor(20, 20, 20)
		}
	}

	// Closing disclaimer page.
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(0, 8, "Disclaimer", "", 1, "L", false, 0, "")
	pdf.Ln(1)
	pdf.SetFont("Helvetica", "", 10)
	pdf.MultiCell(0, 6, Disclaimer, "", "L", false)

	return pdf.OutputFileAndClose(path)
}
