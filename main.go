// Vodafone Invoice Downloader
// Downloads Vodafone invoices (Mobilfunk/Kabel) and sends them via email
package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const Version = "1.3.0"

var cfg Config

var contractTypes = map[string]string{
	"mobilfunk": "Mobilfunk",
	"kabel":     "Kabel",
}

var contractFileNames = map[string]string{
	"mobilfunk": "Mobil",
	"kabel":     "Kabel",
}

var months = map[string]string{
	"Januar": "01", "Februar": "02", "März": "03", "April": "04",
	"Mai": "05", "Juni": "06", "Juli": "07", "August": "08",
	"September": "09", "Oktober": "10", "November": "11", "Dezember": "12",
}

var monthNames = []string{"", "Januar", "Februar", "März", "April", "Mai", "Juni",
	"Juli", "August", "September", "Oktober", "November", "Dezember"}

type Config struct {
	VodafoneUser string `json:"vodafone_user"`
	VodafonePass string `json:"vodafone_pass"`
	EmailUser    string `json:"email_user"`
	EmailPass    string `json:"email_pass"`
	EmailTo      string `json:"email_to"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     string `json:"smtp_port"`
}

type InvoiceInfo struct {
	Filename  string
	Month     string
	Year      string
	MonthName string
	Type      string
	PDFData   []byte
}

func main() {
	if err := loadConfig(); err != nil {
		log.Fatalf("Config error: %v", err)
	}

	// Launch headless Chrome and log into Vodafone
	ctx, cancel := createBrowserContext()
	defer cancel()

	log.Println("Logging in...")
	if err := login(ctx); err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	now := time.Now()
	targetMonth := fmt.Sprintf("%s %d", monthNames[now.Month()], now.Year())
	log.Printf("Looking for invoices: %s", targetMonth)

	// Try to download invoices for each contract type (Mobilfunk, Kabel)
	var results []InvoiceInfo
	for contractType, typeName := range contractTypes {
		log.Printf("Searching %s...", typeName)
		if inv := downloadInvoice(ctx, contractType, typeName); inv != nil {
			results = append(results, *inv)
		}
	}

	// Send all found invoices as email attachments
	if len(results) > 0 {
		log.Println("Sending email...")
		if err := sendEmail(results); err != nil {
			log.Printf("Email failed: %v", err)
		} else {
			log.Printf("Done: %d invoice(s) sent", len(results))
		}
	} else {
		log.Println("No invoices found")
	}
}

func loadConfig() error {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &cfg)
}

// createBrowserContext starts a headless Chrome instance with a 5-minute timeout.
// Returns a context and a cleanup function that shuts down Chrome.
func createBrowserContext() (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx,
		chromedp.WithErrorf(func(string, ...interface{}) {}), // suppress noisy chromedp errors
	)
	ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Minute)

	return ctx, func() {
		timeoutCancel()
		ctxCancel()
		allocCancel()
	}
}

// login navigates to the Vodafone login page, dismisses the cookie banner,
// and submits the credentials from config.
func login(ctx context.Context) error {
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.vodafone.de/meinvodafone/account/login"),
		chromedp.WaitVisible(`#username-text`, chromedp.ByID),
	); err != nil {
		return err
	}

	// Dismiss cookie consent banner (ignore error if not present)
	chromedp.Run(ctx, chromedp.Click(`#dip-consent-summary-reject-all`, chromedp.ByID))
	time.Sleep(time.Second)

	return chromedp.Run(ctx,
		chromedp.SendKeys(`#username-text`, cfg.VodafoneUser, chromedp.ByID),
		chromedp.SendKeys(`#passwordField-input`, cfg.VodafonePass, chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.Sleep(5*time.Second),
	)
}

// isInvoiceReady checks if the "Aktuelle Rechnung" button on the page is enabled.
// A disabled button means the invoice for the current billing period isn't ready yet.
// Returns true if the button is enabled or not found (falls through to other checks).
func isInvoiceReady(ctx context.Context) bool {
	var disabled bool
	chromedp.Run(ctx, chromedp.Evaluate(`
		(() => {
			const btn = [...document.querySelectorAll('button')].find(b =>
				b.innerText.includes('Aktuelle Rechnung'));
			if (!btn) return true;
			return btn.disabled || btn.getAttribute('aria-disabled') === 'true' ||
				btn.classList.contains('disabled');
		})()
	`, &disabled))
	return !disabled
}

// downloadInvoice navigates to the invoice page for a contract type, checks if the
// invoice is ready, verifies it matches the current month/year, and captures the PDF.
// Returns nil if the invoice is not available or doesn't match the current period.
func downloadInvoice(ctx context.Context, contractType, typeName string) *InvoiceInfo {
	if err := navigateToInvoicePage(ctx, typeName); err != nil {
		return nil
	}

	// Early exit if the "Aktuelle Rechnung" button is disabled (invoice not yet generated)
	if !isInvoiceReady(ctx) {
		log.Printf("%s invoice not yet available", typeName)
		return nil
	}

	now := time.Now()
	currentMonth := fmt.Sprintf("%02d", now.Month())
	currentYear := fmt.Sprintf("%d", now.Year())
	monthYear := fmt.Sprintf("%s %s", monthNames[now.Month()], currentYear)

	// Extract page text and parse invoice month/year to verify it's the current period
	var pageText string
	chromedp.Run(ctx, chromedp.Text(`body`, &pageText, chromedp.ByQuery))

	info := parseInvoiceInfo(pageText)
	if info == nil || info.Month != currentMonth || info.Year != currentYear {
		log.Printf("%s %s not found!", typeName, monthYear)
		return nil
	}

	log.Printf("Downloading %s %s...", typeName, monthYear)

	// Intercept the PDF blob and capture its data
	pdfData, err := capturePDF(ctx)
	if err != nil {
		log.Printf("%s %s failed!", typeName, monthYear)
		return nil
	}

	info.Type = typeName
	info.Filename = fmt.Sprintf("%s_%s_Rechnung_Vodafone_%s.pdf", info.Month, info.Year, contractFileNames[contractType])
	info.PDFData = pdfData
	return info
}

// navigateToInvoicePage goes to the Vodafone services page, selects the contract
// card (e.g. "Mobilfunk-Vertrag"), then clicks "Meine Rechnungen" to open the invoice view.
func navigateToInvoicePage(ctx context.Context, typeName string) error {
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.vodafone.de/meinvodafone/services/"),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return err
	}

	// Find the contract card by matching h2 text (e.g. "Mobilfunk-Vertrag") and click it
	contractName := typeName + "-Vertrag"
	chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`
			document.querySelectorAll('h2').forEach(h => {
				if (h.innerText.includes('%s')) (h.closest('a') || h.parentElement).click();
			});
		`, contractName), nil),
		chromedp.Sleep(3*time.Second),
	)

	// Click the "Meine Rechnungen" link/button to navigate to the invoice page
	return chromedp.Run(ctx,
		chromedp.Evaluate(`
			[...document.querySelectorAll('a, button')].find(el =>
				el.innerText.includes('Rechnungen'))?.click();
		`, nil),
		chromedp.Sleep(3*time.Second),
	)
}

// capturePDF intercepts the browser's PDF blob creation to capture the invoice data.
// It hooks URL.createObjectURL to grab any PDF blob, then clicks the download button
// to trigger the PDF generation, and finally extracts the base64-encoded PDF data.
func capturePDF(ctx context.Context) ([]byte, error) {
	// Hook URL.createObjectURL to intercept PDF blobs before they become download URLs
	chromedp.Run(ctx, chromedp.Evaluate(`
		window._capturedPDFs = [];
		if (!window._origCreateObjectURL) window._origCreateObjectURL = URL.createObjectURL;
		URL.createObjectURL = function(blob) {
			if (blob?.type === 'application/pdf') {
				const reader = new FileReader();
				reader.onload = () => window._capturedPDFs.push(reader.result);
				reader.readAsDataURL(blob);
			}
			return window._origCreateObjectURL.call(URL, blob);
		};
	`, nil))

	// Click the download button to trigger PDF generation
	chromedp.Run(ctx, chromedp.Evaluate(`
		[...document.querySelectorAll('button')].find(btn =>
			btn.innerText.includes('Rechnung herunterladen') ||
			btn.innerText.includes('PDF'))?.click();
	`, nil))

	// Wait for the PDF blob to be generated and captured by our hook
	time.Sleep(5 * time.Second)

	// Retrieve captured PDF data from our hook
	var captured []string
	chromedp.Run(ctx, chromedp.Evaluate(`window._capturedPDFs || []`, &captured))

	if len(captured) == 0 {
		return nil, fmt.Errorf("no PDF captured")
	}

	// Decode from base64 data URL to raw PDF bytes
	pdfBase64 := strings.TrimPrefix(captured[0], "data:application/pdf;base64,")
	return base64.StdEncoding.DecodeString(pdfBase64)
}

// parseInvoiceInfo extracts the invoice month and year from page text using regex.
// Tries multiple patterns to match different Vodafone page layouts (e.g. "Rechnung Februar 2026"
// or "Rechnungsdatum: 01. Februar 2026"). Returns nil if no match is found.
func parseInvoiceInfo(text string) *InvoiceInfo {
	patterns := []string{
		`Rechnung (\w+) (\d{4})`,
		`Rechnungsdatum[:\s]+\d+\.\s*(\w+)\s+(\d{4})`,
	}

	for _, pattern := range patterns {
		if matches := regexp.MustCompile(pattern).FindStringSubmatch(text); len(matches) >= 3 {
			if month, ok := months[matches[1]]; ok {
				return &InvoiceInfo{Month: month, Year: matches[2], MonthName: matches[1]}
			}
		}
	}
	return nil
}

// sendEmail builds a MIME multipart email with all invoice PDFs as attachments
// and sends it via SMTP/TLS using the credentials from config.
func sendEmail(invoices []InvoiceInfo) error {
	// Build the plain-text body listing all invoices
	var body strings.Builder
	body.WriteString("Anbei Deine Vodafone Rechnungen:\n\n")
	for _, inv := range invoices {
		body.WriteString(fmt.Sprintf("- %s: %s %s\n", inv.Type, inv.MonthName, inv.Year))
	}

	// Construct MIME multipart message with PDF attachments
	boundary := "==VODAFONE_BOUNDARY=="
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Deine Rechnungen von Vodafone\r\n", cfg.EmailUser, cfg.EmailTo))
	msg.WriteString(fmt.Sprintf("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))
	msg.WriteString(fmt.Sprintf("--%s\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n%s\r\n", boundary, body.String()))

	for _, inv := range invoices {
		if len(inv.PDFData) == 0 {
			continue
		}
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString(fmt.Sprintf("Content-Type: application/pdf\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"%s\"\r\n\r\n", inv.Filename))
		msg.WriteString(base64.StdEncoding.EncodeToString(inv.PDFData))
		msg.WriteString("\r\n")
	}
	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	// Connect to SMTP server via TLS
	conn, err := tls.Dial("tcp", cfg.SMTPHost+":"+cfg.SMTPPort, &tls.Config{ServerName: cfg.SMTPHost})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()

	// Authenticate and send the email
	if err := client.Auth(smtp.PlainAuth("", cfg.EmailUser, cfg.EmailPass, cfg.SMTPHost)); err != nil {
		return err
	}
	if err := client.Mail(cfg.EmailUser); err != nil {
		return err
	}
	if err := client.Rcpt(cfg.EmailTo); err != nil {
		return err
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	w.Write([]byte(msg.String()))
	w.Close()
	client.Quit()
	return nil
}
