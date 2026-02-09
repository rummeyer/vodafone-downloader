# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- Configuration via `config.json`
- In-memory PDF handling (no disk I/O)
- Progress messages with month/year info
- Headless Chrome automation via chromedp

### Technical

- Extracted `navigateToInvoicePage()` for contract navigation
- Extracted `capturePDF()` for PDF blob interception
- Contract type mapping for easy extensibility
