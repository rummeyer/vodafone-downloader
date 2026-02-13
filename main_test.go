package main

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
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
