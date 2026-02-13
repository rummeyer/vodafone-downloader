# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.6.0] - 2026-02-13

### Fixed

- Headless Chrome blocked by Vodafone's bot detection, causing login timeout

### Changed

- Switched from legacy `--headless` to Chrome's new headless mode (`--headless=new`)
- Added anti-detection measures: custom user agent, `AutomationControlled` blink feature disabled, `navigator.webdriver` property removed via CDP

## [1.5.0] - 2026-02-10

### Fixed

- Invoice detection for new Vodafone page layout (button text changed from "Aktuelle Rechnung" to "Rechnung (PDF)"/"Rechnung herunterladen")
- Mobilfunk page content not loading due to insufficient wait time
- `parseInvoiceInfo` regex not matching months with umlauts (e.g. März) — changed `\w+` to `\p{L}+`

### Changed

- Replaced fixed 3s sleep with content polling (up to 15s) for invoice page loading
- Removed unreliable `isInvoiceReady` check
- Default email subject changed to "Deine PDF-Rechnungen von Vodafone"

### Added

- Configurable email subject via `email_subject` in `config.yaml`
- Archive fallback: when current month's PDF download fails, automatically downloads the first entry from Rechnungsarchiv
- `parseArchiveFirstEntry` function to extract month/year from archive entries
- `capturePDF` now accepts click JS as parameter for flexible button targeting
- Tests for `parseInvoiceInfo` (8 cases) and `parseArchiveFirstEntry` (7 cases)
- Test for custom email subject

## [1.4.0] - 2026-02-09

### Changed

- Replaced raw SMTP/TLS email sending with gomail library
- Extracted `buildMessage` function from `sendEmail` for testability

### Added

- Unit tests for email building (`TestBuildMessage` with table-driven cases)
- Test for invalid SMTP port error handling (`TestSendEmailInvalidPort`)

## [1.3.0] - 2026-02-09

### Added

- Check if "Aktuelle Rechnung" button is disabled before attempting download (removed in 1.5.0)
- Early exit with clear message when invoice is not yet available
- Comments on all functions for better readability

## [1.2.0] - 2026-02-09

### Changed

- PDF filename format changed to `MM_YYYY_Rechnung_Vodafone_Type.pdf` (e.g. `02_2026_Rechnung_Vodafone_Mobil.pdf`)

## [1.1.0] - 2026-02-09

### Changed

- Only download invoices for current month
- Detect invoice month from page content instead of assuming previous month
- Simplified code structure (~35% reduction)
- Consolidated contract type handling
- Improved console output messages

### Fixed

- Correct month displayed in "not found" messages

## [1.0.0] - 2026-02-09

### Added

- Initial release
- Login to Vodafone MeinVodafone portal
- Download Mobilfunk invoices
- Download Kabel invoices
- Send invoices via email with PDF attachments
- Configuration via `config.yaml`
- In-memory PDF handling (no disk I/O)
- Progress messages with month/year info
- Headless Chrome automation via chromedp

### Technical

- Extracted `navigateToInvoicePage()` for contract navigation
- Extracted `capturePDF()` for PDF blob interception
- Contract type mapping for easy extensibility
