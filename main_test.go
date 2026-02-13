package main

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	tests := []struct {
		name              string
		invoices          []InvoiceInfo
		wantSubject       string
		wantBodyContains  []string
		wantAttachments   []string // expected filenames
		wantNoAttachments bool
	}{
		{
			name: "single invoice",
			invoices: []InvoiceInfo{
				{
					Filename:  "02_2026_Rechnung_Vodafone_Mobilfunk.pdf",
					Month:     "02",
					Year:      "2026",
					MonthName: "Februar",
					Type:      "Mobilfunk",
					PDFData:   []byte("%PDF-fake-content"),
				},
			},
			wantSubject:      "Deine PDF-Rechnungen von Vodafone",
			wantBodyContains: []string{"Mobilfunk: Februar 2026"},
			wantAttachments:  []string{"02_2026_Rechnung_Vodafone_Mobilfunk.pdf"},
		},
		{
			name: "multiple invoices",
			invoices: []InvoiceInfo{
				{
					Filename:  "02_2026_Rechnung_Vodafone_Mobilfunk.pdf",
					Month:     "02",
					Year:      "2026",
					MonthName: "Februar",
					Type:      "Mobilfunk",
					PDFData:   []byte("%PDF-mobilfunk"),
				},
				{
					Filename:  "02_2026_Rechnung_Vodafone_Kabel.pdf",
					Month:     "02",
					Year:      "2026",
					MonthName: "Februar",
					Type:      "Kabel",
					PDFData:   []byte("%PDF-kabel"),
				},
			},
			wantSubject: "Deine PDF-Rechnungen von Vodafone",
			wantBodyContains: []string{
				"Mobilfunk: Februar 2026",
				"Kabel: Februar 2026",
			},
			wantAttachments: []string{
				"02_2026_Rechnung_Vodafone_Mobilfunk.pdf",
				"02_2026_Rechnung_Vodafone_Kabel.pdf",
			},
		},
		{
			name: "invoice with empty PDFData is skipped",
			invoices: []InvoiceInfo{
				{
					Filename:  "02_2026_Rechnung_Vodafone_Mobilfunk.pdf",
					Month:     "02",
					Year:      "2026",
					MonthName: "Februar",
					Type:      "Mobilfunk",
					PDFData:   nil,
				},
			},
			wantSubject:       "Deine PDF-Rechnungen von Vodafone",
			wantBodyContains:  []string{"Mobilfunk: Februar 2026"},
			wantNoAttachments: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg = Config{
				Email: EmailConfig{From: "sender@example.com", To: "recipient@example.com"},
				SMTP:  SMTPConfig{Host: "smtp.example.com", Port: "587", User: "sender@example.com", Pass: "pass"},
			}

			m := buildMessage(tc.invoices)

			// Verify headers
			if got := m.GetHeader("From"); len(got) != 1 || got[0] != "sender@example.com" {
				t.Errorf("From = %v, want [sender@example.com]", got)
			}
			if got := m.GetHeader("To"); len(got) != 1 || got[0] != "recipient@example.com" {
				t.Errorf("To = %v, want [recipient@example.com]", got)
			}
			if got := m.GetHeader("Subject"); len(got) != 1 || got[0] != tc.wantSubject {
				t.Errorf("Subject = %v, want [%s]", got, tc.wantSubject)
			}

			// Write message to buffer and parse as MIME
			var buf bytes.Buffer
			if _, err := m.WriteTo(&buf); err != nil {
				t.Fatalf("WriteTo failed: %v", err)
			}

			msg, err := mail.ReadMessage(&buf)
			if err != nil {
				t.Fatalf("ReadMessage failed: %v", err)
			}

			contentType := msg.Header.Get("Content-Type")
			mediaType, params, err := mime.ParseMediaType(contentType)
			if err != nil {
				t.Fatalf("ParseMediaType failed: %v", err)
			}

			if tc.wantNoAttachments {
				// Without attachments, gomail produces a simple message (no multipart/mixed)
				body, err := io.ReadAll(msg.Body)
				if err != nil {
					t.Fatalf("ReadAll body failed: %v", err)
				}
				bodyStr := string(body)
				for _, want := range tc.wantBodyContains {
					if !strings.Contains(bodyStr, want) {
						t.Errorf("body missing %q", want)
					}
				}
				return
			}

			if !strings.HasPrefix(mediaType, "multipart/") {
				t.Fatalf("expected multipart, got %s", mediaType)
			}

			reader := multipart.NewReader(msg.Body, params["boundary"])
			var bodyText string
			var attachmentNames []string

			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("NextPart failed: %v", err)
				}

				partData, err := io.ReadAll(part)
				if err != nil {
					t.Fatalf("ReadAll part failed: %v", err)
				}

				disposition := part.Header.Get("Content-Disposition")
				if strings.HasPrefix(disposition, "attachment") {
					_, dParams, _ := mime.ParseMediaType(disposition)
					attachmentNames = append(attachmentNames, dParams["filename"])
				} else {
					bodyText += string(partData)
				}
			}

			for _, want := range tc.wantBodyContains {
				if !strings.Contains(bodyText, want) {
					t.Errorf("body missing %q, got: %s", want, bodyText)
				}
			}

			if len(attachmentNames) != len(tc.wantAttachments) {
				t.Fatalf("got %d attachments %v, want %d %v",
					len(attachmentNames), attachmentNames,
					len(tc.wantAttachments), tc.wantAttachments)
			}
			for i, want := range tc.wantAttachments {
				if attachmentNames[i] != want {
					t.Errorf("attachment[%d] = %q, want %q", i, attachmentNames[i], want)
				}
			}
		})
	}
}

func TestBuildMessageCustomSubject(t *testing.T) {
	cfg = Config{
		Email: EmailConfig{From: "sender@example.com", To: "recipient@example.com", Subject: "Custom Subject"},
	}

	m := buildMessage([]InvoiceInfo{{
		Filename: "test.pdf", Month: "02", Year: "2026",
		MonthName: "Februar", Type: "Mobilfunk", PDFData: nil,
	}})

	if got := m.GetHeader("Subject"); len(got) != 1 || got[0] != "Custom Subject" {
		t.Errorf("Subject = %v, want [Custom Subject]", got)
	}
}

func TestParseInvoiceInfo(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantMonth string
		wantYear  string
		wantNil   bool
	}{
		{
			name:      "Aktuelle Rechnung format",
			text:      "Aktuelle Rechnung Februar 2026\nRechnung vom 10.02.2026",
			wantMonth: "02",
			wantYear:  "2026",
		},
		{
			name:      "Rechnungsdatum format",
			text:      "Rechnungsdatum: 01. Januar 2026\nKosten: 24,98€",
			wantMonth: "01",
			wantYear:  "2026",
		},
		{
			name:      "Rechnung März with special char",
			text:      "Aktuelle Rechnung März 2026",
			wantMonth: "03",
			wantYear:  "2026",
		},
		{
			name:      "Dezember end of year",
			text:      "Aktuelle Rechnung Dezember 2025\nRechnung vom 10.12.2025",
			wantMonth: "12",
			wantYear:  "2025",
		},
		{
			name:    "no match in text",
			text:    "Willkommen bei Vodafone. Keine Rechnung vorhanden.",
			wantNil: true,
		},
		{
			name:    "empty text",
			text:    "",
			wantNil: true,
		},
		{
			name:    "unknown month name",
			text:    "Aktuelle Rechnung January 2026",
			wantNil: true,
		},
		{
			name:      "picks first match",
			text:      "Aktuelle Rechnung Februar 2026\nRechnungsdatum: 15. Januar 2025",
			wantMonth: "02",
			wantYear:  "2026",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parseInvoiceInfo(tc.text)
			if tc.wantNil {
				if info != nil {
					t.Errorf("expected nil, got month=%s year=%s", info.Month, info.Year)
				}
				return
			}
			if info == nil {
				t.Fatal("expected InvoiceInfo, got nil")
			}
			if info.Month != tc.wantMonth {
				t.Errorf("Month = %q, want %q", info.Month, tc.wantMonth)
			}
			if info.Year != tc.wantYear {
				t.Errorf("Year = %q, want %q", info.Year, tc.wantYear)
			}
		})
	}
}

func TestParseArchiveFirstEntry(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantMonth string
		wantYear  string
		wantName  string
		wantNil   bool
	}{
		{
			name: "typical archive page",
			text: `Aktuelle Rechnung Februar 2026
Rechnungsarchiv
Datum	Betrag	Rechnung
Januar
04.01.2026
24,98 €
Rechnung (PDF)
Dezember
04.12.2025
24,98 €`,
			wantMonth: "01",
			wantYear:  "2026",
			wantName:  "Januar",
		},
		{
			name: "März entry with umlaut",
			text: `Rechnungsarchiv
März
15.03.2026
44,98 €`,
			wantMonth: "03",
			wantYear:  "2026",
			wantName:  "März",
		},
		{
			name: "picks first entry not second",
			text: `Rechnungsarchiv
November
10.11.2025
44,98 €
Oktober
09.10.2025
44,98 €`,
			wantMonth: "11",
			wantYear:  "2025",
			wantName:  "November",
		},
		{
			name:    "no Rechnungsarchiv section",
			text:    "Aktuelle Rechnung Februar 2026\nKeine weiteren Rechnungen.",
			wantNil: true,
		},
		{
			name:    "Rechnungsarchiv but no entries",
			text:    "Rechnungsarchiv\nKeine Rechnungen vorhanden.",
			wantNil: true,
		},
		{
			name:    "empty text",
			text:    "",
			wantNil: true,
		},
		{
			name: "ignores current invoice before archive section",
			text: `Aktuelle Rechnung Februar 2026
Rechnung vom 10.02.2026
Rechnungsarchiv
Januar
04.01.2026
24,98 €`,
			wantMonth: "01",
			wantYear:  "2026",
			wantName:  "Januar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parseArchiveFirstEntry(tc.text)
			if tc.wantNil {
				if info != nil {
					t.Errorf("expected nil, got month=%s year=%s", info.Month, info.Year)
				}
				return
			}
			if info == nil {
				t.Fatal("expected InvoiceInfo, got nil")
			}
			if info.Month != tc.wantMonth {
				t.Errorf("Month = %q, want %q", info.Month, tc.wantMonth)
			}
			if info.Year != tc.wantYear {
				t.Errorf("Year = %q, want %q", info.Year, tc.wantYear)
			}
			if info.MonthName != tc.wantName {
				t.Errorf("MonthName = %q, want %q", info.MonthName, tc.wantName)
			}
		})
	}
}

func TestSendEmailInvalidPort(t *testing.T) {
	cfg = Config{
		Email: EmailConfig{From: "sender@example.com", To: "recipient@example.com"},
		SMTP:  SMTPConfig{Host: "smtp.example.com", Port: "not-a-number", User: "sender@example.com", Pass: "pass"},
	}

	err := sendEmail([]InvoiceInfo{
		{
			Filename:  "test.pdf",
			Month:     "02",
			Year:      "2026",
			MonthName: "Februar",
			Type:      "Mobilfunk",
			PDFData:   []byte("%PDF-test"),
		},
	})

	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
	if !strings.Contains(err.Error(), "invalid SMTP port") {
		t.Errorf("error = %q, want it to contain 'invalid SMTP port'", err.Error())
	}
}

func TestSendEmailEmptyPort(t *testing.T) {
	cfg = Config{
		Email: EmailConfig{From: "a@b.com", To: "c@d.com"},
		SMTP:  SMTPConfig{Host: "smtp.example.com", Port: "", User: "u", Pass: "p"},
	}

	err := sendEmail([]InvoiceInfo{{
		Filename: "test.pdf", Month: "01", Year: "2026",
		MonthName: "Januar", Type: "Mobilfunk", PDFData: []byte("%PDF"),
	}})

	if err == nil {
		t.Fatal("expected error for empty port, got nil")
	}
	if !strings.Contains(err.Error(), "invalid SMTP port") {
		t.Errorf("error = %q, want it to contain 'invalid SMTP port'", err.Error())
	}
}

func TestLoadConfig(t *testing.T) {
	// Save and restore global cfg and working directory
	origCfg := cfg
	defer func() { cfg = origCfg }()

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	t.Run("valid config", func(t *testing.T) {
		dir := t.TempDir()
		os.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
vodafone:
  user: "testuser"
  pass: "testpass"
email:
  from: "a@b.com"
  to: "c@d.com"
  subject: "Test Subject"
smtp:
  host: "smtp.test.com"
  port: "465"
  user: "smtpuser"
  pass: "smtppass"
`), 0644)

		cfg = Config{}
		if err := loadConfig(); err != nil {
			t.Fatalf("loadConfig() error: %v", err)
		}
		if cfg.Vodafone.User != "testuser" {
			t.Errorf("Vodafone.User = %q, want %q", cfg.Vodafone.User, "testuser")
		}
		if cfg.Vodafone.Pass != "testpass" {
			t.Errorf("Vodafone.Pass = %q, want %q", cfg.Vodafone.Pass, "testpass")
		}
		if cfg.Email.From != "a@b.com" {
			t.Errorf("Email.From = %q, want %q", cfg.Email.From, "a@b.com")
		}
		if cfg.Email.To != "c@d.com" {
			t.Errorf("Email.To = %q, want %q", cfg.Email.To, "c@d.com")
		}
		if cfg.Email.Subject != "Test Subject" {
			t.Errorf("Email.Subject = %q, want %q", cfg.Email.Subject, "Test Subject")
		}
		if cfg.SMTP.Host != "smtp.test.com" {
			t.Errorf("SMTP.Host = %q, want %q", cfg.SMTP.Host, "smtp.test.com")
		}
		if cfg.SMTP.Port != "465" {
			t.Errorf("SMTP.Port = %q, want %q", cfg.SMTP.Port, "465")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		os.Chdir(dir)

		cfg = Config{}
		if err := loadConfig(); err == nil {
			t.Fatal("expected error for missing config file, got nil")
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		dir := t.TempDir()
		os.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`{{{invalid`), 0644)

		cfg = Config{}
		if err := loadConfig(); err == nil {
			t.Fatal("expected error for invalid YAML, got nil")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		os.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(""), 0644)

		cfg = Config{}
		if err := loadConfig(); err != nil {
			t.Fatalf("loadConfig() error on empty file: %v", err)
		}
		// All fields should be zero values
		if cfg.Vodafone.User != "" {
			t.Errorf("Vodafone.User = %q, want empty", cfg.Vodafone.User)
		}
	})

	t.Run("partial config", func(t *testing.T) {
		dir := t.TempDir()
		os.Chdir(dir)
		os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
vodafone:
  user: "onlyuser"
`), 0644)

		cfg = Config{}
		if err := loadConfig(); err != nil {
			t.Fatalf("loadConfig() error: %v", err)
		}
		if cfg.Vodafone.User != "onlyuser" {
			t.Errorf("Vodafone.User = %q, want %q", cfg.Vodafone.User, "onlyuser")
		}
		if cfg.Vodafone.Pass != "" {
			t.Errorf("Vodafone.Pass = %q, want empty", cfg.Vodafone.Pass)
		}
		if cfg.SMTP.Host != "" {
			t.Errorf("SMTP.Host = %q, want empty", cfg.SMTP.Host)
		}
	})
}

func TestParseInvoiceInfoAllMonths(t *testing.T) {
	for monthName, monthNum := range months {
		t.Run(monthName, func(t *testing.T) {
			text := "Aktuelle Rechnung " + monthName + " 2026"
			info := parseInvoiceInfo(text)
			if info == nil {
				t.Fatalf("expected InvoiceInfo for %s, got nil", monthName)
			}
			if info.Month != monthNum {
				t.Errorf("Month = %q, want %q", info.Month, monthNum)
			}
			if info.Year != "2026" {
				t.Errorf("Year = %q, want %q", info.Year, "2026")
			}
			if info.MonthName != monthName {
				t.Errorf("MonthName = %q, want %q", info.MonthName, monthName)
			}
		})
	}
}

func TestParseInvoiceInfoRechnungsdatumAllMonths(t *testing.T) {
	for monthName, monthNum := range months {
		t.Run(monthName, func(t *testing.T) {
			text := "Rechnungsdatum: 15. " + monthName + " 2025"
			info := parseInvoiceInfo(text)
			if info == nil {
				t.Fatalf("expected InvoiceInfo for %s, got nil", monthName)
			}
			if info.Month != monthNum {
				t.Errorf("Month = %q, want %q", info.Month, monthNum)
			}
			if info.Year != "2025" {
				t.Errorf("Year = %q, want %q", info.Year, "2025")
			}
		})
	}
}

func TestParseInvoiceInfoEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantMonth string
		wantYear  string
		wantNil   bool
	}{
		{
			name:    "month name with lowercase",
			text:    "Aktuelle Rechnung februar 2026",
			wantNil: true,
		},
		{
			name:    "year too short",
			text:    "Aktuelle Rechnung Februar 26",
			wantNil: true,
		},
		{
			name:      "extra whitespace in Rechnungsdatum",
			text:      "Rechnungsdatum:  01.  März  2026",
			wantMonth: "03",
			wantYear:  "2026",
		},
		{
			name:      "Rechnungsdatum without colon",
			text:      "Rechnungsdatum 01. April 2026",
			wantMonth: "04",
			wantYear:  "2026",
		},
		{
			name:      "text with lots of surrounding content",
			text:      "Hallo Nutzer\nDein Vertrag\nDetails\nAktuelle Rechnung Oktober 2025\nRechnung vom 01.10.2025\nBetrag: 39,99€\nMehr anzeigen",
			wantMonth: "10",
			wantYear:  "2025",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parseInvoiceInfo(tc.text)
			if tc.wantNil {
				if info != nil {
					t.Errorf("expected nil, got month=%s year=%s", info.Month, info.Year)
				}
				return
			}
			if info == nil {
				t.Fatal("expected InvoiceInfo, got nil")
			}
			if info.Month != tc.wantMonth {
				t.Errorf("Month = %q, want %q", info.Month, tc.wantMonth)
			}
			if info.Year != tc.wantYear {
				t.Errorf("Year = %q, want %q", info.Year, tc.wantYear)
			}
		})
	}
}

func TestParseArchiveFirstEntryEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantMonth string
		wantYear  string
		wantName  string
		wantNil   bool
	}{
		{
			name:    "unknown month in archive",
			text:    "Rechnungsarchiv\nJanuary\n04.01.2026\n24,98 €",
			wantNil: true,
		},
		{
			name: "all months parseable in archive",
			text: `Rechnungsarchiv
Dezember
15.12.2025
44,98 €`,
			wantMonth: "12",
			wantYear:  "2025",
			wantName:  "Dezember",
		},
		{
			name:    "Rechnungsarchiv with only header text",
			text:    "Rechnungsarchiv\nDatum\tBetrag\tRechnung",
			wantNil: true,
		},
		{
			name: "multiple archive sections picks from first",
			text: `Rechnungsarchiv
April
01.04.2026
30,00 €
Rechnungsarchiv
Mai
01.05.2026
35,00 €`,
			wantMonth: "04",
			wantYear:  "2026",
			wantName:  "April",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parseArchiveFirstEntry(tc.text)
			if tc.wantNil {
				if info != nil {
					t.Errorf("expected nil, got month=%s year=%s", info.Month, info.Year)
				}
				return
			}
			if info == nil {
				t.Fatal("expected InvoiceInfo, got nil")
			}
			if info.Month != tc.wantMonth {
				t.Errorf("Month = %q, want %q", info.Month, tc.wantMonth)
			}
			if info.Year != tc.wantYear {
				t.Errorf("Year = %q, want %q", info.Year, tc.wantYear)
			}
			if info.MonthName != tc.wantName {
				t.Errorf("MonthName = %q, want %q", info.MonthName, tc.wantName)
			}
		})
	}
}

func TestBuildMessageEmptyInvoices(t *testing.T) {
	cfg = Config{
		Email: EmailConfig{From: "a@b.com", To: "c@d.com"},
	}

	m := buildMessage([]InvoiceInfo{})

	if got := m.GetHeader("Subject"); len(got) != 1 || got[0] != "Deine PDF-Rechnungen von Vodafone" {
		t.Errorf("Subject = %v, want default subject", got)
	}

	// With no attachments, the message should still be valid
	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	// Verify it's a parseable email message
	if _, err := mail.ReadMessage(&buf); err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
}

func TestBuildMessageAttachmentContent(t *testing.T) {
	cfg = Config{
		Email: EmailConfig{From: "a@b.com", To: "c@d.com"},
	}

	pdfContent := []byte("%PDF-1.4 test content here")
	m := buildMessage([]InvoiceInfo{{
		Filename:  "01_2026_Rechnung_Vodafone_Mobilfunk.pdf",
		Month:     "01",
		Year:      "2026",
		MonthName: "Januar",
		Type:      "Mobilfunk",
		PDFData:   pdfContent,
	}})

	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	msg, err := mail.ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	contentType := msg.Header.Get("Content-Type")
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("ParseMediaType failed: %v", err)
	}

	reader := multipart.NewReader(msg.Body, params["boundary"])
	var foundAttachment bool
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart failed: %v", err)
		}

		disposition := part.Header.Get("Content-Disposition")
		if strings.HasPrefix(disposition, "attachment") {
			foundAttachment = true
			data, _ := io.ReadAll(part)
			// Attachment is base64-encoded by gomail, just verify it's non-empty
			if len(data) == 0 {
				t.Error("attachment data should not be empty")
			}
		}
	}
	if !foundAttachment {
		t.Error("expected at least one attachment")
	}
}

func TestMonthsMapCompleteness(t *testing.T) {
	expectedMonths := map[string]string{
		"Januar": "01", "Februar": "02", "März": "03", "April": "04",
		"Mai": "05", "Juni": "06", "Juli": "07", "August": "08",
		"September": "09", "Oktober": "10", "November": "11", "Dezember": "12",
	}

	if len(months) != 12 {
		t.Errorf("months map has %d entries, want 12", len(months))
	}

	for name, num := range expectedMonths {
		if got, ok := months[name]; !ok {
			t.Errorf("months map missing %q", name)
		} else if got != num {
			t.Errorf("months[%q] = %q, want %q", name, got, num)
		}
	}
}

func TestMonthNamesCompleteness(t *testing.T) {
	if len(monthNames) != 13 {
		t.Fatalf("monthNames has %d entries, want 13 (index 0 is empty)", len(monthNames))
	}

	if monthNames[0] != "" {
		t.Errorf("monthNames[0] = %q, want empty string", monthNames[0])
	}

	expected := []string{"", "Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember"}

	for i, want := range expected {
		if monthNames[i] != want {
			t.Errorf("monthNames[%d] = %q, want %q", i, monthNames[i], want)
		}
	}
}

func TestContractTypes(t *testing.T) {
	if len(contractTypes) != 2 {
		t.Errorf("contractTypes has %d entries, want 2", len(contractTypes))
	}

	if contractTypes["mobilfunk"] != "Mobilfunk" {
		t.Errorf("contractTypes[mobilfunk] = %q, want %q", contractTypes["mobilfunk"], "Mobilfunk")
	}
	if contractTypes["kabel"] != "Kabel" {
		t.Errorf("contractTypes[kabel] = %q, want %q", contractTypes["kabel"], "Kabel")
	}
}

func TestMonthsAndMonthNamesConsistency(t *testing.T) {
	// Verify that every entry in monthNames (except index 0) has a corresponding months entry
	for i := 1; i < len(monthNames); i++ {
		name := monthNames[i]
		num, ok := months[name]
		if !ok {
			t.Errorf("monthNames[%d] = %q has no entry in months map", i, name)
			continue
		}
		expected := fmt.Sprintf("%02d", i)
		if num != expected {
			t.Errorf("months[%q] = %q, want %q (index %d)", name, num, expected, i)
		}
	}
}
