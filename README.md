# Vodafone Invoice Downloader

Downloads Vodafone invoices (Mobilfunk and Kabel) and sends them via email.

## Features

- Downloads current month invoices for Mobilfunk and Kabel contracts
- Archive fallback: if current month's download fails, grabs the latest invoice from the Rechnungsarchiv
- Configurable email subject (optional, has default)
- Sends all invoices in a single email with PDF attachments
- Headless Chrome automation with bot-detection evasion (new headless mode, custom user agent, webdriver flag removal)
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

Copy `config.sample.yaml` to `config.yaml` and fill in your credentials:

```yaml
vodafone:
  user: "your-vodafone-email@example.com"
  pass: "your-vodafone-password"

email:
  from: "sender@example.com"
  to: "recipient@example.com"
  subject: "Deine PDF-Rechnungen von Vodafone"

smtp:
  host: "smtp.example.com"
  port: "465"
  user: "your-smtp-email@example.com"
  pass: "your-smtp-password"
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
