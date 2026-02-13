// Vodafone Invoice Downloader
// Downloads Vodafone invoices (Mobilfunk/Kabel) and sends them via email
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	gomail "gopkg.in/gomail.v2"
	"gopkg.in/yaml.v3"
)

const Version = "1.7.0"

var cfg Config

var contractTypes = map[string]string{
	"mobilfunk": "Mobilfunk",
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
	Vodafone VodafoneConfig `yaml:"vodafone"`
	Email    EmailConfig    `yaml:"email"`
	SMTP     SMTPConfig     `yaml:"smtp"`
}

type VodafoneConfig struct {
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type EmailConfig struct {
	From    string `yaml:"from"`
	To      string `yaml:"to"`
	Subject string `yaml:"subject"`
}

type SMTPConfig struct {
	Host string `yaml:"host"`
	Port string `yaml:"port"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
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
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &cfg)
}

// createBrowserContext starts a headless Chrome instance with a 5-minute timeout.
// Returns a context and a cleanup function that shuts down Chrome.
func createBrowserContext() (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
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
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Remove webdriver flag before any page scripts run
			_, err := page.AddScriptToEvaluateOnNewDocument(`
				Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
			`).Do(ctx)
			return err
		}),
		chromedp.Navigate("https://www.vodafone.de/meinvodafone/account/login"),
		chromedp.WaitVisible(`#username-text`, chromedp.ByID),
	); err != nil {
		return err
	}

	// Dismiss cookie consent banner (ignore error if not present)
	chromedp.Run(ctx, chromedp.Click(`#dip-consent-summary-reject-all`, chromedp.ByID))
	time.Sleep(time.Second)

	return chromedp.Run(ctx,
		chromedp.SendKeys(`#username-text`, cfg.Vodafone.User, chromedp.ByID),
		chromedp.SendKeys(`#passwordField-input`, cfg.Vodafone.Pass, chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.Sleep(5*time.Second),
	)
}

// downloadInvoice navigates to the invoice page for a contract type and tries to
// download the current month's invoice. If that fails, falls back to the first
// entry in the Rechnungsarchiv (typically the previous month).
func downloadInvoice(ctx context.Context, contractType, typeName string) *InvoiceInfo {
	if err := navigateToInvoicePage(ctx, typeName); err != nil {
		return nil
	}

	var pageText string
	chromedp.Run(ctx, chromedp.Text(`body`, &pageText, chromedp.ByQuery))

	now := time.Now()
	currentMonth := fmt.Sprintf("%02d", now.Month())
	currentYear := fmt.Sprintf("%d", now.Year())

	// Try current month's invoice first
	info := parseInvoiceInfo(pageText)
	if info != nil && info.Month == currentMonth && info.Year == currentYear {
		log.Printf("Downloading %s %s %s...", typeName, info.MonthName, info.Year)
		pdfData, err := capturePDF(ctx, clickCurrentInvoice)
		if err == nil {
			info.Type = typeName
			info.Filename = fmt.Sprintf("%s_%s_Rechnung_Vodafone_%s.pdf", info.Month, info.Year, contractTypes[contractType])
			info.PDFData = pdfData
			return info
		}
		log.Printf("%s current invoice download failed, trying archive...", typeName)
	}

	// Fallback: download the first entry from Rechnungsarchiv
	archiveInfo := parseArchiveFirstEntry(pageText)
	if archiveInfo == nil {
		log.Printf("%s: no archive entry found", typeName)
		return nil
	}

	log.Printf("Downloading %s %s %s from archive...", typeName, archiveInfo.MonthName, archiveInfo.Year)
	pdfData, err := capturePDF(ctx, clickFirstArchiveEntry)
	if err != nil {
		log.Printf("%s archive download failed!", typeName)
		return nil
	}

	archiveInfo.Type = typeName
	archiveInfo.Filename = fmt.Sprintf("%s_%s_Rechnung_Vodafone_%s.pdf", archiveInfo.Month, archiveInfo.Year, contractTypes[contractType])
	archiveInfo.PDFData = pdfData
	return archiveInfo
}

// JS to click the current invoice download button (force-enable if disabled)
const clickCurrentInvoice = `(() => {
	const btn = [...document.querySelectorAll('button')].find(btn =>
		btn.innerText.includes('Rechnung herunterladen') ||
		(btn.innerText.includes('Rechnung') && btn.classList.contains('ws10-button--primary')));
	if (btn) {
		btn.disabled = false;
		btn.classList.remove('ws10-button--disabled', 'disabled');
		btn.removeAttribute('aria-disabled');
		btn.click();
	}
})()`

// JS to click the first "Rechnung (PDF)" link in the archive section
const clickFirstArchiveEntry = `(() => {
	const links = [...document.querySelectorAll('button, a')].filter(b =>
		b.innerText.trim() === 'Rechnung (PDF)' &&
		b.classList.contains('ws10-button-link'));
	if (links.length > 0) links[0].click();
})()`

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
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`
			[...document.querySelectorAll('a, button')].find(el =>
				el.innerText.includes('Rechnungen'))?.click();
		`, nil),
	); err != nil {
		return err
	}

	// Wait for invoice content to load (poll for up to 15 seconds)
	for i := 0; i < 15; i++ {
		time.Sleep(time.Second)
		var hasContent bool
		chromedp.Run(ctx, chromedp.Evaluate(`
			document.body.innerText.includes('Aktuelle Rechnung') ||
			document.body.innerText.includes('Deine Rechnungen')
		`, &hasContent))
		if hasContent {
			return nil
		}
	}
	return nil
}

// capturePDF intercepts the browser's PDF blob creation to capture the invoice data.
// It hooks URL.createObjectURL to grab any PDF blob, executes the provided clickJS
// to trigger the PDF generation, and finally extracts the base64-encoded PDF data.
func capturePDF(ctx context.Context, clickJS string) ([]byte, error) {
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

	// Click the download button/link to trigger PDF generation
	chromedp.Run(ctx, chromedp.Evaluate(clickJS, nil))

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

// parseArchiveFirstEntry extracts the month and year of the first archive entry
// from the Rechnungsarchiv section (e.g. "Januar\n04.01.2026" → month=01, year=2026).
func parseArchiveFirstEntry(text string) *InvoiceInfo {
	idx := strings.Index(text, "Rechnungsarchiv")
	if idx == -1 {
		return nil
	}
	archiveText := text[idx:]

	allMonths := "Januar|Februar|März|April|Mai|Juni|Juli|August|September|Oktober|November|Dezember"
	pattern := regexp.MustCompile(`(` + allMonths + `)\s+\d{2}\.\d{2}\.(\d{4})`)
	matches := pattern.FindStringSubmatch(archiveText)
	if len(matches) < 3 {
		return nil
	}
	monthName := matches[1]
	year := matches[2]
	month, ok := months[monthName]
	if !ok {
		return nil
	}
	return &InvoiceInfo{Month: month, Year: year, MonthName: monthName}
}

// parseInvoiceInfo extracts the invoice month and year from page text using regex.
// Tries multiple patterns to match different Vodafone page layouts (e.g. "Rechnung Februar 2026"
// or "Rechnungsdatum: 01. Februar 2026"). Returns nil if no match is found.
func parseInvoiceInfo(text string) *InvoiceInfo {
	patterns := []string{
		`Rechnung (\p{L}+) (\d{4})`,
		`Rechnungsdatum[:\s]+\d+\.\s*(\p{L}+)\s+(\d{4})`,
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

// buildMessage constructs the email message with invoice details and PDF attachments.
func buildMessage(invoices []InvoiceInfo) *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", cfg.Email.From)
	m.SetHeader("To", cfg.Email.To)
	subject := cfg.Email.Subject
	if subject == "" {
		subject = "Deine PDF-Rechnungen von Vodafone"
	}
	m.SetHeader("Subject", subject)

	// Build the plain-text body listing all invoices
	var body strings.Builder
	body.WriteString("Dokumente anbei.\n")
	for _, inv := range invoices {
		body.WriteString(fmt.Sprintf("- %s: %s %s\n", inv.Type, inv.MonthName, inv.Year))
	}
	m.SetBody("text/plain", body.String())

	// Attach each invoice PDF from its in-memory byte slice
	for _, inv := range invoices {
		if len(inv.PDFData) == 0 {
			continue
		}
		pdfData := inv.PDFData
		m.Attach(inv.Filename, gomail.SetCopyFunc(func(w io.Writer) error {
			_, err := w.Write(pdfData)
			return err
		}))
	}

	return m
}

// sendEmail builds an email with all invoice PDFs as attachments
// and sends it via SMTP/TLS using the credentials from config.
func sendEmail(invoices []InvoiceInfo) error {
	port, err := strconv.Atoi(cfg.SMTP.Port)
	if err != nil {
		return fmt.Errorf("invalid SMTP port: %v", err)
	}

	m := buildMessage(invoices)
	d := gomail.NewDialer(cfg.SMTP.Host, port, cfg.SMTP.User, cfg.SMTP.Pass)
	return d.DialAndSend(m)
}
