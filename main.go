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

const Version = "1.0.0"

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

	ctx, cancel := createBrowserContext()
	defer cancel()

	log.Println("Logging in...")
	if err := login(ctx); err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	now := time.Now()
	targetMonth := fmt.Sprintf("%s %d", monthNames[now.Month()], now.Year())
	log.Printf("Looking for invoices: %s", targetMonth)

	var results []InvoiceInfo
	for contractType, typeName := range contractTypes {
		log.Printf("Searching %s...", typeName)
		if inv := downloadInvoice(ctx, contractType, typeName); inv != nil {
			results = append(results, *inv)
		}
	}

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

func createBrowserContext() (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx,
		chromedp.WithErrorf(func(string, ...interface{}) {}),
	)
	ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Minute)

	return ctx, func() {
		timeoutCancel()
		ctxCancel()
		allocCancel()
	}
}

func login(ctx context.Context) error {
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.vodafone.de/meinvodafone/account/login"),
		chromedp.WaitVisible(`#username-text`, chromedp.ByID),
	); err != nil {
		return err
	}

	chromedp.Run(ctx, chromedp.Click(`#dip-consent-summary-reject-all`, chromedp.ByID))
	time.Sleep(time.Second)

	return chromedp.Run(ctx,
		chromedp.SendKeys(`#username-text`, cfg.VodafoneUser, chromedp.ByID),
		chromedp.SendKeys(`#passwordField-input`, cfg.VodafonePass, chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.Sleep(5*time.Second),
	)
}

func downloadInvoice(ctx context.Context, contractType, typeName string) *InvoiceInfo {
	if err := navigateToInvoicePage(ctx, typeName); err != nil {
		return nil
	}

	now := time.Now()
	currentMonth := fmt.Sprintf("%02d", now.Month())
	currentYear := fmt.Sprintf("%d", now.Year())
	monthYear := fmt.Sprintf("%s %s", monthNames[now.Month()], currentYear)

	var pageText string
	chromedp.Run(ctx, chromedp.Text(`body`, &pageText, chromedp.ByQuery))

	info := parseInvoiceInfo(pageText)
	if info == nil || info.Month != currentMonth || info.Year != currentYear {
		log.Printf("%s %s not found!", typeName, monthYear)
		return nil
	}

	log.Printf("Downloading %s %s...", typeName, monthYear)

	pdfData, err := capturePDF(ctx)
	if err != nil {
		log.Printf("%s %s not found!", typeName, monthYear)
		return nil
	}

	info.Type = typeName
	info.Filename = fmt.Sprintf("vodafone-%s-rechnung-%s-%s.pdf", contractType, info.Month, info.Year)
	info.PDFData = pdfData
	return info
}

func navigateToInvoicePage(ctx context.Context, typeName string) error {
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.vodafone.de/meinvodafone/services/"),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return err
	}

	// Click contract card
	contractName := typeName + "-Vertrag"
	chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`
			document.querySelectorAll('h2').forEach(h => {
				if (h.innerText.includes('%s')) (h.closest('a') || h.parentElement).click();
			});
		`, contractName), nil),
		chromedp.Sleep(3*time.Second),
	)

	// Click "Meine Rechnungen"
	return chromedp.Run(ctx,
		chromedp.Evaluate(`
			[...document.querySelectorAll('a, button')].find(el =>
				el.innerText.includes('Rechnungen'))?.click();
		`, nil),
		chromedp.Sleep(3*time.Second),
	)
}

func capturePDF(ctx context.Context) ([]byte, error) {
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

	chromedp.Run(ctx, chromedp.Evaluate(`
		[...document.querySelectorAll('button')].find(btn =>
			btn.innerText.includes('Rechnung herunterladen') ||
			btn.innerText.includes('PDF'))?.click();
	`, nil))

	time.Sleep(5 * time.Second)

	var captured []string
	chromedp.Run(ctx, chromedp.Evaluate(`window._capturedPDFs || []`, &captured))

	if len(captured) == 0 {
		return nil, fmt.Errorf("no PDF captured")
	}

	pdfBase64 := strings.TrimPrefix(captured[0], "data:application/pdf;base64,")
	return base64.StdEncoding.DecodeString(pdfBase64)
}

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

func sendEmail(invoices []InvoiceInfo) error {
	var body strings.Builder
	body.WriteString("Anbei Deine Vodafone Rechnungen:\n\n")
	for _, inv := range invoices {
		body.WriteString(fmt.Sprintf("- %s: %s %s\n", inv.Type, inv.MonthName, inv.Year))
	}

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
