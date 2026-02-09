// Vodafone Invoice Downloader
// Automatically downloads Vodafone invoices (Mobilfunk and Kabel) and sends them via email.
// Uses headless Chrome browser automation via chromedp.
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

const Version = "1.0.0"

// Config holds all configuration loaded from config.json
type Config struct {
	VodafoneUser string `json:"vodafone_user"` // Vodafone account username
	VodafonePass string `json:"vodafone_pass"` // Vodafone account password
	EmailUser    string `json:"email_user"`    // SMTP sender email address
	EmailPass    string `json:"email_pass"`    // SMTP password
	EmailTo      string `json:"email_to"`      // Recipient email address
	SMTPHost     string `json:"smtp_host"`     // SMTP server hostname
	SMTPPort     string `json:"smtp_port"`     // SMTP server port (usually 465 for TLS)
}

// Global configuration
var cfg Config

// Months maps German month names to numeric values for filename generation
var Months = map[string]string{
	"Januar": "01", "Februar": "02", "März": "03", "April": "04",
	"Mai": "05", "Juni": "06", "Juli": "07", "August": "08",
	"September": "09", "Oktober": "10", "November": "11", "Dezember": "12",
}

// germanMonth returns the German name for a month number (1-12)
func germanMonth(m int) string {
	names := []string{"", "Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember"}
	if m >= 1 && m <= 12 {
		return names[m]
	}
	return ""
}

// InvoiceInfo holds metadata and data for a downloaded invoice
type InvoiceInfo struct {
	Filename  string // Filename for email attachment
	Month     string // Numeric month (01-12)
	Year      string // Four-digit year
	MonthName string // German month name (for email body)
	Type      string // Contract type: "Mobilfunk" or "Kabel"
	PDFData   []byte // PDF content in memory
}

// loadConfig reads and parses config.json from the current directory
func loadConfig() error {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return fmt.Errorf("read config.json: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config.json: %w", err)
	}
	return nil
}

func main() {
	// Load configuration from config.json
	if err := loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Configure Chrome browser options for headless operation
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	// Create browser context with configured options
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx,
		chromedp.WithErrorf(func(string, ...interface{}) {}), // Suppress chromedp errors
	)
	defer cancel()

	// Set overall timeout for the entire operation (5 minutes)
	ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Login to Vodafone account
	log.Println("Logging in...")
	if err := login(ctx); err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	// Download invoices for both contract types
	var results []InvoiceInfo

	// Download Mobilfunk (mobile) invoice
	log.Println("Searching Mobilfunk...")
	if inv := downloadInvoice(ctx, "mobilfunk"); inv != nil {
		results = append(results, *inv)
	}

	// Download Kabel (cable/internet) invoice
	log.Println("Searching Kabel...")
	if inv := downloadInvoice(ctx, "kabel"); inv != nil {
		results = append(results, *inv)
	}

	// Send one email with all invoices attached
	if len(results) > 0 {
		log.Println("Sending email...")
		if err := sendEmailWithAllInvoices(results); err != nil {
			log.Printf("Email failed: %v", err)
		} else {
			log.Printf("Done: %d invoice(s) sent", len(results))
		}
	} else {
		log.Println("No invoices downloaded")
	}
}

// login authenticates with the Vodafone website
func login(ctx context.Context) error {
	err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.vodafone.de/meinvodafone/account/login"),
		chromedp.WaitVisible(`#username-text`, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("navigate to login: %w", err)
	}

	// Dismiss cookie consent banner if present
	chromedp.Run(ctx, chromedp.Click(`#dip-consent-summary-reject-all`, chromedp.ByID))
	time.Sleep(1 * time.Second)

	// Fill in credentials and submit
	err = chromedp.Run(ctx,
		chromedp.SendKeys(`#username-text`, cfg.VodafoneUser, chromedp.ByID),
		chromedp.SendKeys(`#passwordField-input`, cfg.VodafonePass, chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.Sleep(5*time.Second),
	)
	if err != nil {
		return fmt.Errorf("login form: %w", err)
	}

	return nil
}

// downloadInvoice navigates to the invoice page and downloads the PDF
// contractType should be "mobilfunk" or "kabel"
func downloadInvoice(ctx context.Context, contractType string) *InvoiceInfo {
	typeName := "Mobilfunk"
	if contractType == "kabel" {
		typeName = "Kabel"
	}

	// Navigate to the invoice page for this contract type
	if err := navigateToInvoicePage(ctx, contractType); err != nil {
		log.Printf("%s: navigation failed", typeName)
		return nil
	}

	// Extract invoice date from the page first
	var pageText string
	chromedp.Run(ctx, chromedp.Text(`body`, &pageText, chromedp.ByQuery))

	invoiceInfo := parseInvoiceInfo(pageText)
	if invoiceInfo == nil {
		log.Printf("%s not generated yet!", typeName)
		return nil
	}

	// Check if invoice is for current month
	now := time.Now()
	currentMonth := fmt.Sprintf("%02d", now.Month())
	currentYear := fmt.Sprintf("%d", now.Year())
	currentMonthName := germanMonth(int(now.Month()))

	if invoiceInfo.Month != currentMonth || invoiceInfo.Year != currentYear {
		log.Printf("%s %s %s not yet ready!", typeName, currentMonthName, currentYear)
		return nil
	}

	monthYear := fmt.Sprintf("%s %s", invoiceInfo.MonthName, invoiceInfo.Year)
	log.Printf("Downloading %s %s...", typeName, monthYear)

	// Try to download the current invoice PDF
	pdfData, err := capturePDF(ctx)
	if err != nil {
		log.Printf("%s %s not generated yet!", typeName, monthYear)
		return nil
	}

	invoiceInfo.Type = typeName
	invoiceInfo.Filename = fmt.Sprintf("vodafone-%s-rechnung-%s-%s.pdf", contractType, invoiceInfo.Month, invoiceInfo.Year)
	invoiceInfo.PDFData = pdfData

	return invoiceInfo
}

// navigateToInvoicePage navigates to the invoice page for the given contract type
func navigateToInvoicePage(ctx context.Context, contractType string) error {
	// Go to services page
	err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.vodafone.de/meinvodafone/services/"),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		return err
	}

	// Map contract type to German name on the page
	contractNames := map[string]string{
		"mobilfunk": "Mobilfunk-Vertrag",
		"kabel":     "Kabel-Vertrag",
	}
	contractName, ok := contractNames[contractType]
	if !ok {
		return fmt.Errorf("unknown contract type: %s", contractType)
	}

	// Click on the contract card
	err = chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`
			document.querySelectorAll('h2').forEach(h => {
				if (h.innerText.includes('%s')) {
					(h.closest('a') || h.parentElement).click();
				}
			});
		`, contractName), nil),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		return err
	}

	// Click on "Meine Rechnungen" link
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(function() {
				const links = document.querySelectorAll('a');
				for (const a of links) {
					const text = a.innerText || a.textContent || '';
					if (text.includes('Meine Rechnungen') || text === 'Rechnungen') {
						a.click();
						return true;
					}
				}
				const buttons = document.querySelectorAll('button');
				for (const btn of buttons) {
					const text = btn.innerText || btn.textContent || '';
					if (text.includes('Rechnungen')) {
						btn.click();
						return true;
					}
				}
				return false;
			})();
		`, nil),
		chromedp.Sleep(3*time.Second),
	)

	return err
}

// capturePDF installs a blob interceptor, clicks the download button, and captures the PDF
func capturePDF(ctx context.Context) ([]byte, error) {
	// Install PDF blob interceptor
	chromedp.Run(ctx,
		chromedp.Evaluate(`
			window._capturedPDFs = [];
			if (!window._origCreateObjectURL) {
				window._origCreateObjectURL = URL.createObjectURL;
			}
			URL.createObjectURL = function(blob) {
				if (blob && blob.type === 'application/pdf') {
					const reader = new FileReader();
					reader.onload = () => {
						if (!window._capturedPDFs) window._capturedPDFs = [];
						window._capturedPDFs.push(reader.result);
					};
					reader.readAsDataURL(blob);
				}
				return window._origCreateObjectURL.call(URL, blob);
			};
		`, nil),
	)

	// Click download button for current invoice
	chromedp.Run(ctx,
		chromedp.Evaluate(`
			(function() {
				const buttons = document.querySelectorAll('button');
				for (const btn of buttons) {
					const text = btn.innerText || btn.textContent || '';
					if (text.includes('Rechnung herunterladen') ||
					    text.includes('Rechnung (PDF)') ||
					    text.includes('PDF herunterladen')) {
						btn.click();
						return true;
					}
				}
				return false;
			})();
		`, nil),
	)

	// Wait for PDF blob to be captured
	time.Sleep(5 * time.Second)

	// Get captured PDFs
	var capturedPDFs []string
	chromedp.Run(ctx, chromedp.Evaluate(`window._capturedPDFs || []`, &capturedPDFs))

	if len(capturedPDFs) == 0 {
		return nil, fmt.Errorf("no PDF captured")
	}

	// Decode the first captured PDF
	pdfDataURL := capturedPDFs[0]
	pdfBase64 := strings.TrimPrefix(pdfDataURL, "data:application/pdf;base64,")
	pdfBytes, err := base64.StdEncoding.DecodeString(pdfBase64)
	if err != nil {
		return nil, fmt.Errorf("decode pdf: %w", err)
	}

	return pdfBytes, nil
}

// parseInvoiceInfo extracts month and year from page text
func parseInvoiceInfo(text string) *InvoiceInfo {
	patterns := []string{
		`Aktuelle Rechnung (\w+) (\d{4})`,
		`Rechnung (\w+) (\d{4})`,
		`Rechnungsdatum[:\s]+\d+\.\s*(\w+)\s+(\d{4})`,
		`(\w+)\s+(\d{4})\s+Rechnung`,
		`Rechnung vom \d+\.\s*(\w+)\s+(\d{4})`,
		`(\d{2})\.(\d{4})`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(text)
		if len(matches) >= 3 {
			var month, year, monthName string

			if _, ok := Months[matches[1]]; ok {
				monthName = matches[1]
				month = Months[matches[1]]
				year = matches[2]
			} else if len(matches[1]) == 2 {
				month = matches[1]
				year = matches[2]
				for name, num := range Months {
					if num == month {
						monthName = name
						break
					}
				}
			} else {
				continue
			}

			return &InvoiceInfo{
				Month:     month,
				Year:      year,
				MonthName: monthName,
			}
		}
	}

	return nil
}

// sendEmailWithAllInvoices sends one email with all invoice PDFs attached
func sendEmailWithAllInvoices(invoices []InvoiceInfo) error {
	subject := "Deine Rechnungen von Vodafone"

	var bodyLines []string
	bodyLines = append(bodyLines, "Anbei Deine Vodafone Rechnungen:\n")
	for _, inv := range invoices {
		bodyLines = append(bodyLines, fmt.Sprintf("- %s: %s %s", inv.Type, inv.MonthName, inv.Year))
	}
	body := strings.Join(bodyLines, "\n")

	boundary := "==VODAFONE_BOUNDARY=="
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("From: %s\r\n", cfg.EmailUser))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", cfg.EmailTo))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)
	msg.WriteString("\r\n\r\n")

	for _, inv := range invoices {
		if len(inv.PDFData) == 0 {
			continue
		}
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: application/pdf\r\n")
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", inv.Filename))
		msg.WriteString("\r\n")
		msg.WriteString(base64.StdEncoding.EncodeToString(inv.PDFData))
		msg.WriteString("\r\n")
	}

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost}
	conn, err := tls.Dial("tcp", cfg.SMTPHost+":"+cfg.SMTPPort, tlsConfig)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", cfg.EmailUser, cfg.EmailPass, cfg.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	if err := client.Mail(cfg.EmailUser); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}

	if err := client.Rcpt(cfg.EmailTo); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	_, err = w.Write([]byte(msg.String()))
	if err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}

	client.Quit()
	return nil
}
