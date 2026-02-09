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
			wantSubject:      "Deine Rechnungen von Vodafone",
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
			wantSubject: "Deine Rechnungen von Vodafone",
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
			wantSubject:       "Deine Rechnungen von Vodafone",
			wantBodyContains:  []string{"Mobilfunk: Februar 2026"},
			wantNoAttachments: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg = Config{
				EmailUser: "sender@example.com",
				EmailTo:   "recipient@example.com",
				SMTPHost:  "smtp.example.com",
				SMTPPort:  "587",
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

func TestSendEmailInvalidPort(t *testing.T) {
	cfg = Config{
		EmailUser: "sender@example.com",
		EmailTo:   "recipient@example.com",
		SMTPHost:  "smtp.example.com",
		SMTPPort:  "not-a-number",
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
