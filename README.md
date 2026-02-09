# Vodafone Invoice Downloader

Automatically downloads Vodafone invoices (Mobilfunk and Kabel) and sends them via email.

## Features

- Logs into Vodafone MeinVodafone portal
- Downloads current invoices for Mobilfunk and Kabel contracts
- Sends all invoices in a single email with PDF attachments
- Headless Chrome automation (no visible browser window)
- In-memory PDF handling (no files written to disk)

## Requirements

- Go 1.24 or later
- Google Chrome or Chromium installed

## Installation

```bash
git clone https://github.com/yourusername/vodafone-downloader.git
cd vodafone-downloader
go build -o vodafone-downloader .
```

## Configuration

Create a `config.json` file in the same directory as the binary:

```json
{
  "vodafone_user": "your-vodafone-email@example.com",
  "vodafone_pass": "your-vodafone-password",
  "email_user": "your-smtp-email@example.com",
  "email_pass": "your-smtp-password",
  "email_to": "recipient@example.com",
  "smtp_host": "smtp.example.com",
  "smtp_port": "465"
}
```

| Field | Description |
|-------|-------------|
| `vodafone_user` | Vodafone account email |
| `vodafone_pass` | Vodafone account password |
| `email_user` | SMTP sender email address |
| `email_pass` | SMTP password |
| `email_to` | Recipient email address |
| `smtp_host` | SMTP server hostname |
| `smtp_port` | SMTP server port (465 for TLS) |

## Usage

```bash
./vodafone-downloader
```

### Example Output

```
Logging in...
Searching Mobilfunk Januar 2026...
Downloading Mobilfunk Januar 2026...
Searching Kabel Januar 2026...
Downloading Kabel Januar 2026...
Sending email...
Done: 2 invoice(s) sent
```

If an invoice is not yet generated:

```
Mobilfunk Januar 2026 not generated yet!
```

## Adding New Contract Types

Edit `navigateToInvoicePage()` in `main.go` and add to the `contractNames` map:

```go
contractNames := map[string]string{
    "mobilfunk": "Mobilfunk-Vertrag",
    "kabel":     "Kabel-Vertrag",
    "dsl":       "DSL-Vertrag",  // example
}
```

Then add the download call in `main()`.

## License

MIT License - see [LICENSE](LICENSE)
