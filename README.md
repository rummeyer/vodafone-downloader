# Vodafone Invoice Downloader

Downloads Vodafone invoices (Mobilfunk and Kabel) and sends them via email.

## Features

- Downloads current month invoices for Mobilfunk and Kabel contracts
- Archive fallback: if current month's download fails, grabs the latest invoice from the Rechnungsarchiv
- Sends all invoices in a single email with PDF attachments
- Headless Chrome automation (no visible browser window)
- In-memory PDF handling (no files written to disk)

## Requirements

- Go 1.25+
- Google Chrome or Chromium

## Installation

```bash
git clone https://github.com/rummeyer/vodafone-downloader.git
cd vodafone-downloader
go build -o vodafone-downloader .
```

## Configuration

Copy `config.sample.json` to `config.json` and fill in your credentials:

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

## Usage

```bash
./vodafone-downloader
```

### When to Run

Run the tool at the **end of the month** (around the 25th or later) to ensure all invoices are available in MeinVodafone. Invoices are typically generated mid-month and may not be ready earlier.

### Example Output

```
Logging in...
Looking for invoices: Februar 2026
Searching Mobilfunk...
Downloading Mobilfunk Februar 2026...
Searching Kabel...
Downloading Kabel Februar 2026...
Sending email...
Done: 2 invoice(s) sent
```

If the current month's PDF download is unavailable, the archive fallback kicks in:

```
Searching Mobilfunk...
Downloading Mobilfunk Februar 2026...
Mobilfunk current invoice download failed, trying archive...
Downloading Mobilfunk Januar 2026 from archive...
```

## Adding Contract Types

Edit `contractTypes` map in `main.go`:

```go
var contractTypes = map[string]string{
    "mobilfunk": "Mobilfunk",
    "kabel":     "Kabel",
    "dsl":       "DSL",  // example
}
```

## License

MIT License - see [LICENSE](LICENSE)
